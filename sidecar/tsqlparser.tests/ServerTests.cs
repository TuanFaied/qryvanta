using System.Text.Json;
using Qryvanta.TsqlParser;

namespace TsqlParser.Tests;

public class ServerTests
{
    /// <summary>Runs the server over the given input and returns each output line as JSON.</summary>
    private static List<JsonElement> Run(string input, TextWriter? log = null)
    {
        var output = new StringWriter();
        new Server(new StringReader(input), output, log ?? TextWriter.Null).Run();
        return output.ToString()
            .Split('\n', StringSplitOptions.RemoveEmptyEntries)
            .Select(line => JsonDocument.Parse(line).RootElement.Clone())
            .ToList();
    }

    private static string Req(string id, string op, string? sql = null, int? level = null) =>
        JsonSerializer.Serialize(new Dictionary<string, object?> { ["id"] = id, ["op"] = op, ["sql"] = sql, ["compatLevel"] = level });

    [Fact]
    public void SaysHelloFirst()
    {
        var hello = Assert.Single(Run(""));
        Assert.Equal("hello", hello.GetProperty("type").GetString());
        Assert.Equal(ProtocolInfo.Version, hello.GetProperty("protocol").GetInt32());
        Assert.Equal("qryvanta-tsqlparser", hello.GetProperty("name").GetString());
        Assert.Equal(ScriptParser.DefaultCompatLevel, hello.GetProperty("defaultCompatLevel").GetInt32());
        Assert.Equal(Server.MaxSqlLength, hello.GetProperty("maxSqlLength").GetInt32());
        Assert.NotEmpty(hello.GetProperty("scriptDom").GetString()!);
    }

    [Fact]
    public void AnswersEachRequestInOrder()
    {
        var lines = Run(string.Join("\n", Req("a", "parse", "SELECT 1\nGO\nSELECT FROM"), Req("b", "ping"), Req("c", "parse", "SELECT 3")));
        Assert.Equal(4, lines.Count);
        Assert.Equal(["a", "b", "c"], lines.Skip(1).Select(l => l.GetProperty("id").GetString()));

        var a = lines[1];
        Assert.True(a.GetProperty("ok").GetBoolean());
        Assert.Equal("response", a.GetProperty("type").GetString());
        Assert.Equal(2, a.GetProperty("batches").GetArrayLength());
        Assert.Equal(3, a.GetProperty("diagnostics")[0].GetProperty("line").GetInt32());
        Assert.Equal(ScriptParser.DefaultCompatLevel, a.GetProperty("compatLevel").GetInt32());

        Assert.True(lines[2].GetProperty("ok").GetBoolean());
        Assert.False(lines[2].TryGetProperty("batches", out _));
    }

    [Theory]
    [InlineData("not json", "bad_request")]
    [InlineData("{\"op\":\"parse\",\"sql\":\"SELECT 1\"}", "bad_request")]
    [InlineData("{\"id\":\"1\"}", "bad_request")]
    [InlineData("{\"id\":\"1\",\"op\":\"parse\"}", "bad_request")]
    [InlineData("{\"id\":\"1\",\"op\":\"format\",\"sql\":\"x\"}", "unknown_op")]
    [InlineData("{\"id\":\"1\",\"op\":\"parse\",\"sql\":\"x\",\"compatLevel\":75}", "unsupported_compat_level")]
    [InlineData("null", "bad_request")]
    public void RejectsBadRequestsAndKeepsServing(string request, string code)
    {
        var lines = Run(request + "\n" + Req("next", "ping"));
        Assert.Equal(3, lines.Count);
        Assert.False(lines[1].GetProperty("ok").GetBoolean());
        Assert.Equal(code, lines[1].GetProperty("error").GetProperty("code").GetString());
        Assert.True(lines[2].GetProperty("ok").GetBoolean());
    }

    [Fact]
    public void KeepsTheIdOfAFailedRequest()
    {
        var lines = Run("{\"id\":\"x7\",\"op\":\"nope\"}");
        Assert.Equal("x7", lines[1].GetProperty("id").GetString());
    }

    [Fact]
    public void RejectsOversizedSqlAndRequests()
    {
        var sql = new string('x', Server.MaxSqlLength + 1);
        var lines = Run(Req("big", "parse", sql) + "\n" + Req("after", "ping"));
        Assert.Equal("too_large", lines[1].GetProperty("error").GetProperty("code").GetString());
        Assert.Equal("big", lines[1].GetProperty("id").GetString());
        Assert.True(lines[2].GetProperty("ok").GetBoolean());

        var line = "{\"id\":\"huge\",\"op\":\"parse\",\"sql\":\"" + new string('a', Server.MaxLineLength) + "\"}";
        lines = Run(line + "\n" + Req("after", "ping"));
        Assert.Equal("too_large", lines[1].GetProperty("error").GetProperty("code").GetString());
        Assert.Equal(JsonValueKind.Null, lines[1].GetProperty("id").ValueKind);
        Assert.Equal("after", lines[2].GetProperty("id").GetString());
    }

    [Fact]
    public void IgnoresBlankLinesAndAByteOrderMark()
    {
        var lines = Run("﻿" + Req("1", "ping") + "\r\n\n   \n" + Req("2", "ping") + "\r\n");
        Assert.Equal(3, lines.Count);
        Assert.All(lines.Skip(1), l => Assert.True(l.GetProperty("ok").GetBoolean()));
    }

    [Fact]
    public void WritesTextWithoutUnnecessaryEscapes()
    {
        var output = new StringWriter();
        new Server(new StringReader(Req("1", "parse", "SELECT N'Müller''s <café>'")), output, TextWriter.Null).Run();
        Assert.Contains("SELECT N'Müller''s <café>'", output.ToString());
    }

    [Fact]
    public void OutputIsOneJsonObjectPerLine()
    {
        var output = new StringWriter();
        new Server(new StringReader(Req("1", "parse", "SELECT 1\n\nSELECT 2")), output, TextWriter.Null).Run();
        var text = output.ToString();
        Assert.EndsWith("\n", text);
        Assert.Equal(2, text.Count(c => c == '\n'));
    }
}
