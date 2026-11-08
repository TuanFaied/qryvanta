using Microsoft.SqlServer.TransactSql.ScriptDom;

namespace Qryvanta.TsqlParser;

internal sealed record ParseResult(IReadOnlyList<Batch> Batches, IReadOnlyList<Diagnostic> Diagnostics);

/// <summary>Parses T-SQL scripts with ScriptDom.</summary>
internal static class ScriptParser
{
    /// <summary>SQL Server 2025. Newer levels (180) are accepted when asked for.</summary>
    public const int DefaultCompatLevel = 170;

    public static readonly int[] CompatLevels = [80, 90, 100, 110, 120, 130, 140, 150, 160, 170, 180];

    public static bool IsSupported(int level) => Array.IndexOf(CompatLevels, level) >= 0;

    private static TSqlParser Create(int level) => level switch
    {
        80 => new TSql80Parser(initialQuotedIdentifiers: true),
        90 => new TSql90Parser(true),
        100 => new TSql100Parser(true),
        110 => new TSql110Parser(true),
        120 => new TSql120Parser(true),
        130 => new TSql130Parser(true),
        140 => new TSql140Parser(true),
        150 => new TSql150Parser(true),
        160 => new TSql160Parser(true),
        170 => new TSql170Parser(true),
        180 => new TSql180Parser(true),
        _ => throw new ArgumentOutOfRangeException(nameof(level), level, "unsupported compatibility level"),
    };

    /// <summary>
    /// Splits the script into batches and reports syntax errors. Batches come from the token
    /// stream, so GO inside strings and comments is not a separator, and splitting works even
    /// when the script has syntax errors. Each batch is then parsed on its own, as SQL Server
    /// receives it; GO itself (and GO n, which ScriptDom does not accept) never reaches the
    /// parser. Diagnostic positions are mapped back to the whole script.
    /// </summary>
    public static ParseResult Parse(string sql, int compatLevel)
    {
        var parser = Create(compatLevel);
        var lines = new LineIndex(sql);

        var tokens = parser.GetTokenStream(new StringReader(sql), out _);
        var batches = SplitBatches(sql, tokens, lines);

        var diagnostics = new List<Diagnostic>();
        foreach (var batch in batches)
        {
            parser.Parse(new StringReader(batch.Text), out var errors);
            foreach (var e in errors)
            {
                var offset = batch.Offset + e.Offset;
                var (line, column) = lines.Position(offset);
                diagnostics.Add(new Diagnostic(e.Number, e.Message, offset, line, column));
            }
        }
        return new ParseResult(batches, diagnostics);
    }

    private static List<Batch> SplitBatches(string sql, IList<TSqlParserToken> tokens, LineIndex lines)
    {
        var batches = new List<Batch>();
        var start = 0;
        var hasCode = false;

        void Add(int end, int repeat)
        {
            if (hasCode)
            {
                var (line, column) = lines.Position(start);
                batches.Add(new Batch(batches.Count, sql[start..end], start, end - start, line, column, repeat));
            }
            hasCode = false;
        }

        for (var i = 0; i < tokens.Count; i++)
        {
            var t = tokens[i];
            if (t.TokenType == TSqlTokenType.EndOfFile)
            {
                break;
            }
            if (t.TokenType != TSqlTokenType.Go)
            {
                hasCode |= !IsTrivia(t.TokenType);
                continue;
            }

            // GO [count]: an optional integer on the same line repeats the batch.
            var repeat = 1;
            var j = i + 1;
            while (j < tokens.Count && tokens[j].TokenType == TSqlTokenType.WhiteSpace && !tokens[j].Text.Contains('\n'))
            {
                j++;
            }
            if (j < tokens.Count && tokens[j].TokenType == TSqlTokenType.Integer && int.TryParse(tokens[j].Text, out var n) && n > 0)
            {
                repeat = n;
                i = j;
            }

            Add(t.Offset, repeat);
            // The next batch starts on the line after GO.
            var lineEnd = sql.IndexOf('\n', tokens[i].Offset + tokens[i].Text.Length);
            start = lineEnd < 0 ? sql.Length : lineEnd + 1;
            // Skip the tokens that were on the GO line.
            while (i + 1 < tokens.Count && tokens[i + 1].Offset < start)
            {
                i++;
            }
        }
        Add(sql.Length, 1);
        return batches;
    }

    private static bool IsTrivia(TSqlTokenType type) =>
        type is TSqlTokenType.WhiteSpace or TSqlTokenType.SingleLineComment or TSqlTokenType.MultilineComment;
}

/// <summary>Maps UTF-16 offsets to 1-based line and column numbers.</summary>
internal sealed class LineIndex
{
    private readonly List<int> _starts = [0];

    public LineIndex(string text)
    {
        for (var i = 0; i < text.Length; i++)
        {
            if (text[i] == '\n')
            {
                _starts.Add(i + 1);
            }
        }
    }

    public (int Line, int Column) Position(int offset)
    {
        var index = _starts.BinarySearch(offset);
        if (index < 0)
        {
            index = ~index - 1;
        }
        return (index + 1, offset - _starts[index] + 1);
    }
}
