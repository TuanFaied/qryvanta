using System.Text;

namespace Qryvanta.TsqlParser;

/// <summary>
/// Reads newline-terminated lines without ever buffering more than a limit, so one oversized
/// request cannot exhaust memory. An oversized line is consumed and reported, and reading
/// continues with the next line.
/// </summary>
internal sealed class BoundedLineReader(TextReader reader, int maxLength)
{
    private readonly char[] _buffer = new char[64 * 1024];
    private int _pos;
    private int _len;
    private bool _started;

    /// <summary>The next line without its terminator, or null at end of input.</summary>
    public string? ReadLine(out bool tooLong)
    {
        tooLong = false;
        var sb = new StringBuilder();
        var any = false;
        while (true)
        {
            if (_pos == _len)
            {
                _len = reader.Read(_buffer, 0, _buffer.Length);
                _pos = 0;
                if (_len == 0)
                {
                    return any ? Finish(sb, tooLong) : null;
                }
                // Some shells (Windows PowerShell 5.1) put a byte-order mark before piped text.
                if (!_started && _buffer[0] == '﻿')
                {
                    _pos = 1;
                }
                _started = true;
                if (_pos == _len)
                {
                    continue;
                }
            }
            any = true;
            var newline = Array.IndexOf(_buffer, '\n', _pos, _len - _pos);
            var end = newline < 0 ? _len : newline;
            if (!tooLong)
            {
                if (sb.Length + (end - _pos) > maxLength)
                {
                    tooLong = true;
                    sb.Clear();
                }
                else
                {
                    sb.Append(_buffer, _pos, end - _pos);
                }
            }
            if (newline >= 0)
            {
                _pos = newline + 1;
                return Finish(sb, tooLong);
            }
            _pos = _len;
        }
    }

    private static string Finish(StringBuilder sb, bool tooLong)
    {
        if (tooLong)
        {
            return "";
        }
        if (sb.Length > 0 && sb[^1] == '\r')
        {
            sb.Length--;
        }
        return sb.ToString();
    }
}
