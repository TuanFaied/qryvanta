using System.Reflection;
using System.Text.Json;
using Microsoft.SqlServer.TransactSql.ScriptDom;

namespace Qryvanta.TsqlParser;

/// <summary>The request loop: reads requests from input and writes one response line per request.</summary>
internal sealed class Server(TextReader input, TextWriter output, TextWriter log)
{
    /// <summary>Largest SQL text accepted, in characters (matches the app's 16 MB file limit).</summary>
    public const int MaxSqlLength = 16 * 1024 * 1024;

    /// <summary>
    /// Largest request line. JSON escaping makes a request longer than its SQL; clients must
    /// not escape non-ASCII or HTML characters (Go: Encoder.SetEscapeHTML(false)).
    /// </summary>
    public const int MaxLineLength = 2 * MaxSqlLength + 4096;

    public static string Version =>
        typeof(Server).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion.Split('+')[0]
        ?? "0.0.0";

    private static string ScriptDomVersion =>
        typeof(TSqlParser).Assembly.GetCustomAttribute<AssemblyInformationalVersionAttribute>()?.InformationalVersion.Split('+')[0]
        ?? "unknown";

    /// <summary>Serves requests until the input ends.</summary>
    public void Run()
    {
        Write(JsonSerializer.Serialize(new Hello
        {
            Protocol = ProtocolInfo.Version,
            Name = "qryvanta-tsqlparser",
            Version = Version,
            ScriptDom = ScriptDomVersion,
            DefaultCompatLevel = ScriptParser.DefaultCompatLevel,
            CompatLevels = ScriptParser.CompatLevels,
            MaxSqlLength = MaxSqlLength,
        }, ProtocolJson.Wire.Hello));

        var reader = new BoundedLineReader(input, MaxLineLength);
        while (reader.ReadLine(out var tooLong) is { } line)
        {
            if (!tooLong && string.IsNullOrWhiteSpace(line))
            {
                continue;
            }
            var response = tooLong
                ? Fail(null, ErrorCodes.TooLarge, $"the request is longer than {MaxLineLength} characters")
                : Handle(line);
            Write(JsonSerializer.Serialize(response, ProtocolJson.Wire.Response));
        }
    }

    internal Response Handle(string line)
    {
        Request? request;
        try
        {
            request = JsonSerializer.Deserialize(line, ProtocolJson.Wire.Request);
        }
        catch (JsonException e)
        {
            return Fail(null, ErrorCodes.BadRequest, "the request is not valid JSON: " + e.Message);
        }
        if (request is null)
        {
            return Fail(null, ErrorCodes.BadRequest, "the request is empty");
        }
        if (string.IsNullOrEmpty(request.Id))
        {
            return Fail(null, ErrorCodes.BadRequest, "the request has no id");
        }

        try
        {
            return request.Op switch
            {
                "ping" => new Response { Id = request.Id, Ok = true },
                "parse" => Parse(request),
                null or "" => Fail(request.Id, ErrorCodes.BadRequest, "the request has no op"),
                _ => Fail(request.Id, ErrorCodes.UnknownOp, $"unknown op \"{request.Op}\""),
            };
        }
        catch (Exception e)
        {
            // A bug in parsing must not take the process down; report it and keep serving.
            log.WriteLine($"qryvanta-tsqlparser: request {request.Id} failed: {e}");
            return Fail(request.Id, ErrorCodes.Internal, "internal parser error: " + e.Message);
        }
    }

    private static Response Parse(Request request)
    {
        if (request.Sql is null)
        {
            return Fail(request.Id, ErrorCodes.BadRequest, "parse needs sql");
        }
        if (request.Sql.Length > MaxSqlLength)
        {
            return Fail(request.Id, ErrorCodes.TooLarge, $"the SQL is longer than {MaxSqlLength} characters");
        }
        var level = request.CompatLevel ?? ScriptParser.DefaultCompatLevel;
        if (!ScriptParser.IsSupported(level))
        {
            return Fail(request.Id, ErrorCodes.UnsupportedCompatLevel,
                $"compatibility level {level} is not supported (use one of {string.Join(", ", ScriptParser.CompatLevels)})");
        }
        var result = ScriptParser.Parse(request.Sql, level);
        return new Response
        {
            Id = request.Id,
            Ok = true,
            CompatLevel = level,
            Batches = result.Batches,
            Diagnostics = result.Diagnostics,
        };
    }

    private static Response Fail(string? id, string code, string message) =>
        new() { Id = id, Ok = false, Error = new ProtocolError(code, message) };

    private void Write(string json)
    {
        output.Write(json);
        output.Write('\n');
        output.Flush();
    }
}
