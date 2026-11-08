using Qryvanta.TsqlParser;

namespace TsqlParser.Tests;

public class ScriptParserTests
{
    private static ParseResult Parse(string sql, int level = ScriptParser.DefaultCompatLevel) => ScriptParser.Parse(sql, level);

    [Fact]
    public void SplitsBatchesOnGoAndReportsSyntaxErrorsWithScriptLines()
    {
        var r = Parse("SELECT 1\nGO\nSELECT FROM");
        Assert.Equal(2, r.Batches.Count);
        Assert.Equal("SELECT 1\n", r.Batches[0].Text);
        Assert.Equal(new Batch(1, "SELECT FROM", 12, 11, 3, 1, 1), r.Batches[1]);

        var d = Assert.Single(r.Diagnostics);
        Assert.Equal(3, d.Line);
        Assert.Equal(8, d.Column);
        Assert.Equal(19, d.Offset);
        Assert.Contains("FROM", d.Message);
    }

    [Fact]
    public void GoInsideStringsAndCommentsDoesNotSplit()
    {
        var r = Parse("SELECT 'a\nGO\nb' AS x; -- GO\n/*\nGO\n*/\nSELECT 2");
        var batch = Assert.Single(r.Batches);
        Assert.Equal(0, batch.Offset);
        Assert.Empty(r.Diagnostics);
    }

    [Theory]
    [InlineData("SELECT 1\ngo\nSELECT 2")]
    [InlineData("SELECT 1\n  GO  \nSELECT 2")]
    [InlineData("SELECT 1\r\nGO\r\nSELECT 2")]
    [InlineData("SELECT 1\nGO -- run it\nSELECT 2")]
    public void RecognisesGoVariants(string sql)
    {
        var r = Parse(sql);
        Assert.Equal(2, r.Batches.Count);
        Assert.StartsWith("SELECT 1", r.Batches[0].Text);
        Assert.Equal("SELECT 2", r.Batches[1].Text);
        Assert.Equal(3, r.Batches[1].Line);
        Assert.Empty(r.Diagnostics);
    }

    [Fact]
    public void GoWithCountRepeatsTheBatchWithoutASyntaxError()
    {
        var r = Parse("INSERT INTO #t DEFAULT VALUES;\nGO 5\nSELECT COUNT(*) FROM #t");
        Assert.Equal(2, r.Batches.Count);
        Assert.Equal(5, r.Batches[0].Repeat);
        Assert.Equal(1, r.Batches[1].Repeat);
        Assert.Empty(r.Diagnostics);
    }

    [Fact]
    public void SkipsEmptyBatchesButKeepsIndexesDense()
    {
        var r = Parse("GO\n\n-- only a comment\nGO\nSELECT 1\nGO\nGO\n");
        var batch = Assert.Single(r.Batches);
        Assert.Equal(0, batch.Index);
        Assert.Equal(5, batch.Line);
        Assert.Equal("SELECT 1\n", batch.Text);
    }

    [Fact]
    public void ErrorsInLaterBatchesMapToScriptPositions()
    {
        var sql = "SELECT 1;\nGO\nSELECT 2;\n\nSELECT FROM WHERE;\nGO\nSELECT 3";
        var r = Parse(sql);
        Assert.Equal(3, r.Batches.Count);
        var d = Assert.Single(r.Diagnostics);
        Assert.Equal(5, d.Line);
        Assert.Equal(sql.IndexOf("FROM WHERE", StringComparison.Ordinal), d.Offset);
        Assert.Equal(8, d.Column);
    }

    [Fact]
    public void OffsetsAreUtf16CodeUnits()
    {
        // 🐘 is two UTF-16 code units; the error after it must account for both.
        var sql = "SELECT N'🐘';\nGO\nSELECT FROM";
        var r = Parse(sql);
        Assert.Equal(sql.IndexOf("SELECT FROM", StringComparison.Ordinal), r.Batches[1].Offset);
        Assert.Equal(r.Batches[1].Offset + 7, Assert.Single(r.Diagnostics).Offset);
    }

    [Fact]
    public void EmptyAndWhitespaceScriptsHaveNoBatches()
    {
        Assert.Empty(Parse("").Batches);
        Assert.Empty(Parse("  \n -- nothing\n").Batches);
    }

    [Fact]
    public void CompatLevelSelectsTheGrammar()
    {
        // STRING_AGG ... WITHIN GROUP exists from SQL Server 2017 (140); TRIM from 140 too.
        const string sql = "SELECT TRIM(name) FROM sys.objects";
        Assert.Empty(Parse(sql, 170).Diagnostics);
        Assert.NotEmpty(Parse("SELECT a FROM t ORDER BY a OFFSET 1 ROWS", 100).Diagnostics);
        Assert.Empty(Parse("SELECT a FROM t ORDER BY a OFFSET 1 ROWS", 110).Diagnostics);
    }

    [Fact]
    public void EveryAdvertisedCompatLevelParses()
    {
        foreach (var level in ScriptParser.CompatLevels)
        {
            Assert.Empty(Parse("SELECT 1 AS one", level).Diagnostics);
        }
        Assert.False(ScriptParser.IsSupported(75));
    }

    [Fact]
    public void LineIndexMapsOffsets()
    {
        var index = new LineIndex("ab\ncd\n\nef");
        Assert.Equal((1, 1), index.Position(0));
        Assert.Equal((1, 3), index.Position(2));
        Assert.Equal((2, 1), index.Position(3));
        Assert.Equal((3, 1), index.Position(6));
        Assert.Equal((4, 2), index.Position(8));
    }
}
