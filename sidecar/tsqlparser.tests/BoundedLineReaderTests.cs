using Qryvanta.TsqlParser;

namespace TsqlParser.Tests;

public class BoundedLineReaderTests
{
    private static List<(string Line, bool TooLong)> ReadAll(string input, int max)
    {
        var reader = new BoundedLineReader(new StringReader(input), max);
        var lines = new List<(string, bool)>();
        while (reader.ReadLine(out var tooLong) is { } line)
        {
            lines.Add((line, tooLong));
        }
        return lines;
    }

    [Fact]
    public void ReadsLinesWithAnyEnding()
    {
        Assert.Equal([("a", false), ("b", false), ("", false), ("c", false)], ReadAll("a\r\nb\n\nc", 10));
    }

    [Fact]
    public void FlagsTooLongLinesAndContinues()
    {
        Assert.Equal([("ok", false), ("", true), ("fine", false)], ReadAll("ok\n" + new string('x', 11) + "\nfine\n", 10));
    }

    [Fact]
    public void HandlesLinesLongerThanTheInternalBuffer()
    {
        var big = new string('y', 200_000);
        Assert.Equal([(big, false), ("z", false)], ReadAll(big + "\nz", 300_000));
    }

    [Fact]
    public void DropsALeadingByteOrderMarkOnly()
    {
        Assert.Equal([("a", false), ("﻿b", false)], ReadAll("﻿a\n﻿b", 10));
        Assert.Empty(ReadAll("﻿", 10));
    }
}
