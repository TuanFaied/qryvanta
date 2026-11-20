using System.Text.Json.Serialization;

namespace Qryvanta.TsqlParser;

// QueryModel: Qryvanta's own description of parsed T-SQL (architecture D2). ScriptDom types never
// leave the sidecar. The model is versioned separately from the protocol: ModelVersion changes
// when a field's meaning changes or a field is removed; adding fields or kinds does not change it,
// and consumers must ignore what they do not know.
//
// Conventions:
// - Every node has a Span: script-absolute UTF-16 offset and length, 1-based line and column.
// - Parentheses are not nodes: the wrapped node gets Parenthesized = true, so a consumer sees the
//   real tree (AND binds tighter than OR) and still knows what the author grouped explicitly.
// - Anything not modelled is kind "unsupported" with Type set to the ScriptDom class, never dropped.
// - Identifiers are unquoted ([Order Details] -> Order Details).
// - Empty lists, absent values and false flags are omitted from the JSON.

internal static class ModelInfo
{
    public const int Version = 1;
}

internal sealed record Span(
    [property: JsonPropertyName("offset")] int Offset,
    [property: JsonPropertyName("length")] int Length,
    [property: JsonPropertyName("line")] int Line,
    [property: JsonPropertyName("column")] int Column);

/// <summary>A schema object name: up to server.database.schema.name.</summary>
internal sealed class ObjectName
{
    [JsonPropertyName("server")] public string? Server { get; set; }
    [JsonPropertyName("database")] public string? Database { get; set; }
    [JsonPropertyName("schema")] public string? Schema { get; set; }
    [JsonPropertyName("name")] public string Name { get; set; } = "";
}

/// <summary>
/// A statement. Kind: select, insert, update, delete, merge, truncate, ddl, exec, declare, set,
/// setOption, if, while, block, try, transaction, use, print, waitfor, return, other, unsupported.
/// </summary>
internal sealed class Statement
{
    [JsonPropertyName("kind")] public string Kind { get; set; } = "";
    /// <summary>The ScriptDom statement class, for detail (for example CreateTableStatement).</summary>
    [JsonPropertyName("type")] public string Type { get; set; } = "";
    [JsonPropertyName("span")] public Span Span { get; set; } = null!;
    [JsonPropertyName("ctes")] public List<Cte>? Ctes { get; set; }
    /// <summary>The query of a SELECT, or the source query of INSERT ... SELECT.</summary>
    [JsonPropertyName("query")] public Query? Query { get; set; }
    /// <summary>SELECT ... INTO target.</summary>
    [JsonPropertyName("into")] public ObjectName? Into { get; set; }
    /// <summary>INSERT/UPDATE/DELETE/MERGE/TRUNCATE target.</summary>
    [JsonPropertyName("target")] public Source? Target { get; set; }
    /// <summary>FROM of UPDATE and DELETE.</summary>
    [JsonPropertyName("from")] public Source? From { get; set; }
    /// <summary>WHERE of UPDATE and DELETE.</summary>
    [JsonPropertyName("where")] public Expr? Where { get; set; }
    /// <summary>Condition of IF and WHILE.</summary>
    [JsonPropertyName("condition")] public Expr? Condition { get; set; }
    /// <summary>Statements inside IF (then), WHILE, BEGIN...END and TRY.</summary>
    [JsonPropertyName("body")] public List<Statement>? Body { get; set; }
    /// <summary>ELSE of IF, and the CATCH block of TRY.</summary>
    [JsonPropertyName("else")] public List<Statement>? Else { get; set; }
    /// <summary>EXEC: the procedure name, when it is not dynamic SQL.</summary>
    [JsonPropertyName("procedure")] public ObjectName? Procedure { get; set; }
    /// <summary>Transaction statements: begin, commit, rollback, save.</summary>
    [JsonPropertyName("action")] public string? Action { get; set; }
}

internal sealed class Cte
{
    [JsonPropertyName("name")] public string Name { get; set; } = "";
    [JsonPropertyName("columns")] public List<string>? Columns { get; set; }
    [JsonPropertyName("query")] public Query Query { get; set; } = null!;
    /// <summary>The CTE refers to itself.</summary>
    [JsonPropertyName("recursive")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Recursive { get; set; }
    [JsonPropertyName("span")] public Span Span { get; set; } = null!;
}

/// <summary>
/// A query expression. Kind "select" (one SELECT ... FROM ... WHERE ...) or "setOp" (UNION [ALL],
/// EXCEPT, INTERSECT with Left and Right). ORDER BY and OFFSET/FETCH belong to the whole query.
/// </summary>
internal sealed class Query
{
    [JsonPropertyName("kind")] public string Kind { get; set; } = "";
    [JsonPropertyName("span")] public Span Span { get; set; } = null!;
    [JsonPropertyName("parenthesized")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Parenthesized { get; set; }

    [JsonPropertyName("distinct")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Distinct { get; set; }
    [JsonPropertyName("top")] public Top? Top { get; set; }
    [JsonPropertyName("select")] public List<SelectItem>? Select { get; set; }
    /// <summary>The FROM clause as one tree: a comma list becomes joins of type "comma".</summary>
    [JsonPropertyName("from")] public Source? From { get; set; }
    [JsonPropertyName("where")] public Expr? Where { get; set; }
    [JsonPropertyName("groupBy")] public List<Expr>? GroupBy { get; set; }
    [JsonPropertyName("having")] public Expr? Having { get; set; }

    /// <summary>setOp: union, unionAll, except, intersect.</summary>
    [JsonPropertyName("op")] public string? Op { get; set; }
    [JsonPropertyName("left")] public Query? Left { get; set; }
    [JsonPropertyName("right")] public Query? Right { get; set; }

    [JsonPropertyName("orderBy")] public List<OrderItem>? OrderBy { get; set; }
    [JsonPropertyName("offset")] public Expr? Offset { get; set; }
    [JsonPropertyName("fetch")] public Expr? Fetch { get; set; }
}

internal sealed class Top
{
    [JsonPropertyName("expr")] public Expr Expr { get; set; } = null!;
    [JsonPropertyName("percent")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Percent { get; set; }
    [JsonPropertyName("withTies")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool WithTies { get; set; }
}

/// <summary>A select-list item: kind "expr" (with optional alias) or "star" (* or t.*).</summary>
internal sealed class SelectItem
{
    [JsonPropertyName("kind")] public string Kind { get; set; } = "";
    [JsonPropertyName("expr")] public Expr? Expr { get; set; }
    [JsonPropertyName("alias")] public string? Alias { get; set; }
    /// <summary>For t.*: the qualifier parts.</summary>
    [JsonPropertyName("qualifier")] public List<string>? Qualifier { get; set; }
    [JsonPropertyName("span")] public Span Span { get; set; } = null!;
}

internal sealed class OrderItem
{
    [JsonPropertyName("expr")] public Expr Expr { get; set; } = null!;
    [JsonPropertyName("descending")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Descending { get; set; }
}

/// <summary>
/// A FROM-clause source. Kind: table (Name; Cte when it names a CTE in scope), derived
/// (a subquery: Query), join (JoinType inner/left/right/full/cross/crossApply/outerApply/comma,
/// Left, Right, On), function (table-valued function: Name, Args), variable (@table: Variable),
/// values (a VALUES list), unsupported.
/// </summary>
internal sealed class Source
{
    [JsonPropertyName("kind")] public string Kind { get; set; } = "";
    [JsonPropertyName("span")] public Span Span { get; set; } = null!;
    [JsonPropertyName("parenthesized")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Parenthesized { get; set; }
    [JsonPropertyName("alias")] public string? Alias { get; set; }

    [JsonPropertyName("name")] public ObjectName? Name { get; set; }
    [JsonPropertyName("cte")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Cte { get; set; }
    [JsonPropertyName("query")] public Query? Query { get; set; }
    [JsonPropertyName("columns")] public List<string>? Columns { get; set; }

    [JsonPropertyName("joinType")] public string? JoinType { get; set; }
    [JsonPropertyName("left")] public Source? Left { get; set; }
    [JsonPropertyName("right")] public Source? Right { get; set; }
    [JsonPropertyName("on")] public Expr? On { get; set; }

    [JsonPropertyName("args")] public List<Expr>? Args { get; set; }
    [JsonPropertyName("variable")] public string? Variable { get; set; }
    [JsonPropertyName("type")] public string? Type { get; set; }
}

/// <summary>
/// An expression (scalar or boolean). Kind and the fields it uses:
/// and/or (Left, Right); not (Operand); compare (Op =, &lt;&gt;, !=, &lt;, &gt;, &lt;=, &gt;=, !&lt;, !&gt;; Left, Right;
/// Quantifier any/all with a Query on the right); in (Operand, Values or Query, Not);
/// between (Operand, Low, High, Not); like (Operand, Pattern, Escape, Not); isNull (Operand, Not);
/// exists (Query); column (Parts: the identifier parts; the last is the column, "*" for a wildcard);
/// literal (LiteralType integer/numeric/real/money/binary/string/null/default/max, Value, National);
/// variable (Name); function (Name, Schema, Args, Distinct, Aggregate, Over); subquery (Query);
/// arithmetic (Op +, -, *, /, %, &amp;, |, ^; Left, Right); unary (Op +, -, ~; Operand);
/// cast (Name CAST/CONVERT/TRY_CAST/TRY_CONVERT, DataType, Operand, Args = style);
/// case (Operand for simple CASE, Whens, Else); unsupported (Type).
/// </summary>
internal sealed class Expr
{
    [JsonPropertyName("kind")] public string Kind { get; set; } = "";
    [JsonPropertyName("span")] public Span Span { get; set; } = null!;
    [JsonPropertyName("parenthesized")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Parenthesized { get; set; }

    [JsonPropertyName("op")] public string? Op { get; set; }
    [JsonPropertyName("not")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Not { get; set; }
    [JsonPropertyName("left")] public Expr? Left { get; set; }
    [JsonPropertyName("right")] public Expr? Right { get; set; }
    [JsonPropertyName("operand")] public Expr? Operand { get; set; }
    [JsonPropertyName("values")] public List<Expr>? Values { get; set; }
    [JsonPropertyName("query")] public Query? Query { get; set; }
    [JsonPropertyName("quantifier")] public string? Quantifier { get; set; }
    [JsonPropertyName("low")] public Expr? Low { get; set; }
    [JsonPropertyName("high")] public Expr? High { get; set; }
    [JsonPropertyName("pattern")] public Expr? Pattern { get; set; }
    [JsonPropertyName("escape")] public Expr? Escape { get; set; }

    [JsonPropertyName("parts")] public List<string>? Parts { get; set; }
    [JsonPropertyName("literalType")] public string? LiteralType { get; set; }
    [JsonPropertyName("value")] public string? Value { get; set; }
    [JsonPropertyName("national")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool National { get; set; }

    [JsonPropertyName("name")] public string? Name { get; set; }
    [JsonPropertyName("schema")] public string? Schema { get; set; }
    [JsonPropertyName("args")] public List<Expr>? Args { get; set; }
    [JsonPropertyName("distinct")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Distinct { get; set; }
    [JsonPropertyName("aggregate")] [JsonIgnore(Condition = JsonIgnoreCondition.WhenWritingDefault)] public bool Aggregate { get; set; }
    [JsonPropertyName("over")] public Over? Over { get; set; }
    [JsonPropertyName("dataType")] public string? DataType { get; set; }

    [JsonPropertyName("whens")] public List<When>? Whens { get; set; }
    [JsonPropertyName("else")] public Expr? Else { get; set; }

    [JsonPropertyName("type")] public string? Type { get; set; }
}

internal sealed class Over
{
    [JsonPropertyName("partitionBy")] public List<Expr>? PartitionBy { get; set; }
    [JsonPropertyName("orderBy")] public List<OrderItem>? OrderBy { get; set; }
    /// <summary>The ROWS/RANGE frame as written, if any.</summary>
    [JsonPropertyName("frame")] public string? Frame { get; set; }
}

internal sealed class When
{
    [JsonPropertyName("when")] public Expr Condition { get; set; } = null!;
    [JsonPropertyName("then")] public Expr Then { get; set; } = null!;
}
