// qryvanta-tsqlparser: Qryvanta's T-SQL parser host (architecture decision D2).
//
// It parses T-SQL with Microsoft's ScriptDom and talks newline-delimited JSON over stdin and
// stdout (see Protocol.cs). Qryvanta starts one long-lived process and sends it one request at
// a time. --version prints the version and exits.

using System.Text;
using Qryvanta.TsqlParser;

if (args is ["--version"])
{
    Console.WriteLine($"qryvanta-tsqlparser {Server.Version}");
    return 0;
}
if (args.Length > 0)
{
    Console.Error.WriteLine("usage: qryvanta-tsqlparser [--version]  (requests are read from stdin; see Protocol.cs)");
    return 2;
}

// Explicit UTF-8 without a byte-order mark in both directions, whatever the console code page is.
var utf8 = new UTF8Encoding(encoderShouldEmitUTF8Identifier: false);
using var input = new StreamReader(Console.OpenStandardInput(), utf8, detectEncodingFromByteOrderMarks: false);
using var output = new StreamWriter(Console.OpenStandardOutput(), utf8) { AutoFlush = false, NewLine = "\n" };

new Server(input, output, Console.Error).Run();
return 0;
