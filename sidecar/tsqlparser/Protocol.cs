using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Qryvanta.TsqlParser;

// The wire protocol: newline-delimited JSON on stdin and stdout, one object per line.
//
// The sidecar writes a hello message first, then exactly one response per request line, in
// request order. Offsets and lengths are in UTF-16 code units (as in .NET and JavaScript
// strings); lines and columns are 1-based. Anything not on stdout (diagnostic logging) goes to
// stderr, so stdout only ever carries protocol messages.

internal static class ProtocolInfo
{
    /// <summary>Incremented on any incompatible change to the messages below.</summary>
    public const int Version = 1;
}

internal sealed class Request
{
    [JsonPropertyName("id")] public string? Id { get; set; }
    [JsonPropertyName("op")] public string? Op { get; set; }
    [JsonPropertyName("sql")] public string? Sql { get; set; }
    /// <summary>SQL Server compatibility level (80–180). Missing means the default.</summary>
    [JsonPropertyName("compatLevel")] public int? CompatLevel { get; set; }
}

internal sealed class Hello
{
    [JsonPropertyName("type")] public string Type => "hello";
    [JsonPropertyName("protocol")] public int Protocol { get; init; }
    [JsonPropertyName("name")] public string Name { get; init; } = "";
    [JsonPropertyName("version")] public string Version { get; init; } = "";
    [JsonPropertyName("scriptDom")] public string ScriptDom { get; init; } = "";
    [JsonPropertyName("defaultCompatLevel")] public int DefaultCompatLevel { get; init; }
    [JsonPropertyName("compatLevels")] public int[] CompatLevels { get; init; } = [];
    [JsonPropertyName("maxSqlLength")] public int MaxSqlLength { get; init; }
}

internal sealed class Response
{
    [JsonPropertyName("type")] public string Type => "response";
    [JsonPropertyName("id")] public string? Id { get; init; }
    [JsonPropertyName("ok")] public bool Ok { get; init; }
    [JsonPropertyName("compatLevel")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)]
    public int CompatLevel { get; init; }
    [JsonPropertyName("batches")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public IReadOnlyList<Batch>? Batches { get; init; }
    [JsonPropertyName("diagnostics")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public IReadOnlyList<Diagnostic>? Diagnostics { get; init; }
    [JsonPropertyName("error")]
    [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingNull)]
    public ProtocolError? Error { get; init; }
}

/// <summary>One batch of a script, as separated by GO lines.</summary>
internal sealed record Batch(
    [property: JsonPropertyName("index")] int Index,
    [property: JsonPropertyName("text")] string Text,
    [property: JsonPropertyName("offset")] int Offset,
    [property: JsonPropertyName("length")] int Length,
    [property: JsonPropertyName("line")] int Line,
    [property: JsonPropertyName("column")] int Column,
    /// <summary>GO n runs the batch n times; 1 for a plain GO or the last batch.</summary>
    [property: JsonPropertyName("repeat")] int Repeat);

/// <summary>A syntax error reported by ScriptDom.</summary>
internal sealed record Diagnostic(
    [property: JsonPropertyName("number")] int Number,
    [property: JsonPropertyName("message")] string Message,
    [property: JsonPropertyName("offset")] int Offset,
    [property: JsonPropertyName("line")] int Line,
    [property: JsonPropertyName("column")] int Column);

internal sealed record ProtocolError(
    [property: JsonPropertyName("code")] string Code,
    [property: JsonPropertyName("message")] string Message);

/// <summary>Error codes, part of the protocol.</summary>
internal static class ErrorCodes
{
    public const string BadRequest = "bad_request";
    public const string UnknownOp = "unknown_op";
    public const string TooLarge = "too_large";
    public const string UnsupportedCompatLevel = "unsupported_compat_level";
    public const string Internal = "internal";
}

[JsonSourceGenerationOptions(DefaultIgnoreCondition = JsonIgnoreCondition.Never)]
[JsonSerializable(typeof(Request))]
[JsonSerializable(typeof(Hello))]
[JsonSerializable(typeof(Response))]
internal sealed partial class ProtocolJson : JsonSerializerContext
{
    /// <summary>
    /// The context used on the wire. Output is read by Go, not embedded in HTML, so quotes and
    /// non-ASCII text are written as-is instead of \u escapes (smaller, and readable in logs).
    /// </summary>
    public static ProtocolJson Wire { get; } = new(new JsonSerializerOptions
    {
        Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
    });
}
