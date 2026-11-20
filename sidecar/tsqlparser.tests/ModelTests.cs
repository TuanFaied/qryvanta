using System.Text.Json;
using Qryvanta.TsqlParser;

namespace TsqlParser.Tests;

public class ModelTests
{
    private static List<Statement> Model(string sql)
    {
        var r = ScriptParser.Parse(sql, ScriptParser.DefaultCompatLevel, model: true);
        Assert.Empty(r.Diagnostics);
        return r.Batches.SelectMany(b => b.Statements ?? []).ToList();
    }

    private static Query Select(string sql) => Assert.Single(Model(sql)).Query!;

    private static string Text(string sql, Span s) => sql.Substring(s.Offset, s.Length);

    private static string SampleQuery(string file)
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null && !Directory.Exists(Path.Combine(dir.FullName, "tests", "sample_queries")))
        {
            dir = dir.Parent;
        }
        Assert.NotNull(dir);
        return File.ReadAllText(Path.Combine(dir!.FullName, "tests", "sample_queries", file));
    }

    // ---- Statement kinds ------------------------------------------------------------------

    [Theory]
    [InlineData("SELECT 1", "select")]
    [InlineData("INSERT INTO dbo.t (a) VALUES (1)", "insert")]
    [InlineData("UPDATE t SET a = 1", "update")]
    [InlineData("DELETE FROM t WHERE a = 1", "delete")]
    [InlineData("MERGE t USING s ON t.id = s.id WHEN MATCHED THEN DELETE;", "merge")]
    [InlineData("TRUNCATE TABLE dbo.t", "truncate")]
    [InlineData("CREATE TABLE t (a int)", "ddl")]
    [InlineData("ALTER TABLE t ADD b int", "ddl")]
    [InlineData("DROP TABLE t", "ddl")]
    [InlineData("EXEC dbo.usp_report 1", "exec")]
    [InlineData("EXEC (N'SELECT 1')", "exec")]
    [InlineData("DECLARE @x int", "declare")]
    [InlineData("SET @x = 1", "set")]
    [InlineData("SET NOCOUNT ON", "setOption")]
    [InlineData("SET TRANSACTION ISOLATION LEVEL READ UNCOMMITTED", "setOption")]
    [InlineData("BEGIN TRAN", "transaction")]
    [InlineData("USE master", "use")]
    [InlineData("PRINT 'x'", "print")]
    [InlineData("WAITFOR DELAY '00:00:01'", "waitfor")]
    [InlineData("DBCC CHECKDB", "other")]
    public void ClassifiesStatements(string sql, string kind)
    {
        Assert.Equal(kind, Assert.Single(Model(sql)).Kind);
    }

    [Fact]
    public void TransactionActions()
    {
        Assert.Equal(["begin", "save", "commit", "rollback"],
            Model("BEGIN TRAN; SAVE TRAN s; COMMIT; ROLLBACK;").Select(s => s.Action!));
    }

    [Fact]
    public void NestsStatementsInsideControlFlow()
    {
        var sql = "IF @x > 1 BEGIN SELECT 1; EXEC dbo.p; END ELSE DELETE FROM t; WHILE 1 = 0 BREAK; BEGIN TRY SELECT 2 END TRY BEGIN CATCH ROLLBACK END CATCH";
        var model = Model(sql);
        var ifs = model[0];
        Assert.Equal("if", ifs.Kind);
        Assert.Equal("compare", ifs.Condition!.Kind);
        var block = Assert.Single(ifs.Body!);
        Assert.Equal("block", block.Kind);
        Assert.Equal(["select", "exec"], block.Body!.Select(s => s.Kind));
        Assert.Equal("dbo", block.Body![1].Procedure!.Schema);
        Assert.Equal("p", block.Body![1].Procedure!.Name);
        Assert.Equal("delete", Assert.Single(ifs.Else!).Kind);
        Assert.Equal("while", model[1].Kind);
        Assert.Equal("try", model[2].Kind);
        Assert.Equal("select", Assert.Single(model[2].Body!).Kind);
        Assert.Equal("transaction", Assert.Single(model[2].Else!).Kind);
    }

    [Fact]
    public void DmlTargetsSourcesAndInto()
    {
        var m = Model("SELECT a INTO #tmp FROM t; INSERT INTO dbo.log (a) SELECT a FROM t; UPDATE o SET x = 1 FROM dbo.Orders o JOIN c ON c.id = o.cid WHERE o.id = 2; DELETE dbo.t WHERE a IS NULL");
        Assert.Equal("#tmp", m[0].Into!.Name);
        Assert.Equal("log", m[1].Target!.Name!.Name);
        Assert.Equal("select", m[1].Query!.Kind);
        Assert.Equal("o", m[2].Target!.Name!.Name);
        Assert.Equal("join", m[2].From!.Kind);
        Assert.Equal("compare", m[2].Where!.Kind);
        Assert.Equal("isNull", m[3].Where!.Kind);
    }

    // ---- Queries ----------------------------------------------------------------------------

    [Fact]
    public void SelectClauses()
    {
        var q = Select("SELECT DISTINCT TOP (10) PERCENT WITH TIES a, t.b AS bee, t.* FROM t GROUP BY a, t.b HAVING COUNT(*) > 1 ORDER BY a DESC, bee OFFSET 5 ROWS FETCH NEXT 10 ROWS ONLY");
        Assert.True(q.Distinct);
        Assert.True(q.Top!.Percent);
        Assert.True(q.Top.WithTies);
        Assert.Equal(["expr", "expr", "star"], q.Select!.Select(s => s.Kind));
        Assert.Equal("bee", q.Select![1].Alias);
        Assert.Equal(["t"], q.Select![2].Qualifier!);
        Assert.Equal(2, q.GroupBy!.Count);
        Assert.Equal("compare", q.Having!.Kind);
        Assert.True(q.OrderBy![0].Descending);
        Assert.False(q.OrderBy![1].Descending);
        Assert.Equal("5", q.Offset!.Value);
        Assert.Equal("10", q.Fetch!.Value);
    }

    [Fact]
    public void SetOperationsKeepTheirOrderBy()
    {
        var q = Select("SELECT a FROM t UNION ALL SELECT a FROM u EXCEPT (SELECT a FROM v) ORDER BY a");
        Assert.Equal("setOp", q.Kind);
        Assert.Equal("except", q.Op);
        Assert.Equal("unionAll", q.Left!.Op);
        Assert.True(q.Right!.Parenthesized);
        Assert.Single(q.OrderBy!);
    }

    [Fact]
    public void CtesAreResolvedAndRecursionIsDetected()
    {
        var st = Assert.Single(Model("WITH a AS (SELECT 1 AS n), r (n) AS (SELECT n FROM a UNION ALL SELECT n + 1 FROM r WHERE n < 5) SELECT n FROM r JOIN dbo.a ON 1 = 1"));
        Assert.Equal(["a", "r"], st.Ctes!.Select(c => c.Name));
        Assert.False(st.Ctes![0].Recursive);
        Assert.True(st.Ctes![1].Recursive);
        Assert.Equal(["n"], st.Ctes![1].Columns!);
        var from = st.Query!.From!;
        Assert.True(from.Left!.Cte);         // r
        Assert.False(from.Right!.Cte);       // dbo.a is a table, not the CTE a
    }

    // ---- Sources ----------------------------------------------------------------------------

    [Fact]
    public void JoinTypesAndSources()
    {
        var q = Select("SELECT * FROM [Sales].dbo.[Order Details] od WITH (NOLOCK) " +
            "LEFT JOIN (SELECT id FROM p) AS d (pid) ON d.pid = od.pid " +
            "FULL JOIN @t tv ON 1 = 1 CROSS JOIN q RIGHT JOIN r ON r.a = q.a " +
            "CROSS APPLY dbo.fn(od.id) f OUTER APPLY STRING_SPLIT(od.tags, ',') s " +
            "JOIN (VALUES (1), (2)) v (x) ON v.x = 1");
        var joins = new List<Source>();
        for (var s = q.From; s is { Kind: "join" }; s = s.Left)
        {
            joins.Insert(0, s);
        }
        Assert.Equal(["left", "full", "cross", "right", "crossApply", "outerApply", "inner"], joins.Select(j => j.JoinType!));
        var od = joins[0].Left!;
        Assert.Equal(("Sales", "dbo", "Order Details", "od"), (od.Name!.Database, od.Name.Schema, od.Name.Name, od.Alias));
        Assert.Equal("derived", joins[0].Right!.Kind);
        Assert.Equal(["pid"], joins[0].Right!.Columns!);
        Assert.Equal("variable", joins[1].Right!.Kind);
        Assert.Equal("@t", joins[1].Right!.Variable);
        Assert.Null(joins[2].On);
        Assert.Equal("function", joins[4].Right!.Kind);
        Assert.Equal("fn", joins[4].Right!.Name!.Name);
        Assert.Equal("STRING_SPLIT", joins[5].Right!.Name!.Name);
        Assert.Equal("values", joins[6].Right!.Kind);
    }

    [Fact]
    public void CommaJoinsBecomeJoinsWithoutAPredicate()
    {
        var q = Select("SELECT * FROM a, b x, c");
        Assert.Equal("comma", q.From!.JoinType);
        Assert.Null(q.From.On);
        Assert.Equal("c", q.From.Right!.Name!.Name);
        Assert.Equal("comma", q.From.Left!.JoinType);
        Assert.Equal("x", q.From.Left.Right!.Alias);
    }

    // ---- Expressions ------------------------------------------------------------------------

    [Fact]
    public void PredicatesKeepPrecedenceAndRecordParentheses()
    {
        var sql = "SELECT 1 FROM t WHERE a = 1 OR b = 2 AND (c <> 3) AND NOT (d < 4)";
        var w = Select(sql).Where!;
        Assert.Equal("or", w.Kind);
        Assert.False(w.Parenthesized);
        Assert.Equal("and", w.Right!.Kind);
        Assert.True(w.Right.Left!.Right!.Parenthesized);       // (c <> 3)
        Assert.Equal("<>", w.Right.Left.Right.Op);
        Assert.Equal("not", w.Right.Right!.Kind);
        Assert.Equal("(c <> 3)".Trim('(', ')'), Text(sql, w.Right.Left.Right.Span));
    }

    [Fact]
    public void PredicateKinds()
    {
        var w = Select("SELECT 1 FROM t WHERE a NOT IN (1, 2) AND b IN (SELECT x FROM u) AND c NOT BETWEEN 1 AND 5 " +
            "AND d NOT LIKE 'a%' ESCAPE '!' AND e IS NOT NULL AND EXISTS (SELECT 1 FROM v) AND f > ALL (SELECT y FROM w) AND g != 1").Where!;
        var preds = new List<Expr>();
        void Flatten(Expr e)
        {
            if (e.Kind == "and") { Flatten(e.Left!); Flatten(e.Right!); } else preds.Add(e);
        }
        Flatten(w);
        Assert.Equal(["in", "in", "between", "like", "isNull", "exists", "compare", "compare"], preds.Select(p => p.Kind));
        Assert.True(preds[0].Not);
        Assert.Equal(2, preds[0].Values!.Count);
        Assert.Equal("select", preds[1].Query!.Kind);
        Assert.True(preds[2].Not);
        Assert.Equal("5", preds[2].High!.Value);
        Assert.Equal("!", preds[3].Escape!.Value);
        Assert.True(preds[4].Not);
        Assert.Equal("all", preds[6].Quantifier);
        Assert.Equal("!=", preds[7].Op);
    }

    [Fact]
    public void ScalarExpressions()
    {
        var items = Select("SELECT t.c, @v, @@ROWCOUNT, N'x', NULL, 1.5, -a, (a + b) * 2, COUNT(DISTINCT a), dbo.fn(a), " +
            "CAST(a AS decimal(10, 2)), CONVERT(varchar(10), d, 120), COALESCE(a, b), NULLIF(a, 0), IIF(a > 1, 1, 0), " +
            "CASE WHEN a > 1 THEN 'x' ELSE 'y' END, CASE a WHEN 1 THEN 'one' END, (SELECT MAX(x) FROM u), " +
            "ROW_NUMBER() OVER (PARTITION BY a ORDER BY b DESC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW), CURRENT_TIMESTAMP, LEFT(a, 2), " +
            "t.* FROM t").Select!.Select(s => s.Expr).ToList();
        Assert.Equal(["t", "c"], items[0]!.Parts!);
        Assert.Equal(("variable", "@v"), (items[1]!.Kind, items[1]!.Name));
        Assert.Equal("@@ROWCOUNT", items[2]!.Name);
        Assert.True(items[3]!.National);
        Assert.Equal("null", items[4]!.LiteralType);
        Assert.Equal("numeric", items[5]!.LiteralType);
        Assert.Equal(("unary", "-"), (items[6]!.Kind, items[6]!.Op));
        Assert.Equal("*", items[7]!.Op);
        Assert.True(items[7]!.Left!.Parenthesized);
        Assert.True(items[8]!.Aggregate);
        Assert.True(items[8]!.Distinct);
        Assert.Equal(("fn", "dbo", false), (items[9]!.Name, items[9]!.Schema, items[9]!.Aggregate));
        Assert.Equal(("cast", "CAST", "decimal(10, 2)"), (items[10]!.Kind, items[10]!.Name, items[10]!.DataType));
        Assert.Equal("120", Assert.Single(items[11]!.Args!).Value);
        Assert.Equal("COALESCE", items[12]!.Name);
        Assert.Equal("NULLIF", items[13]!.Name);
        Assert.Equal("IIF", items[14]!.Name);
        Assert.Equal("compare", items[14]!.Args![0].Kind);
        Assert.Equal("case", items[15]!.Kind);
        Assert.Equal("compare", items[15]!.Whens![0].Condition.Kind);
        Assert.Equal("column", items[16]!.Operand!.Kind);
        Assert.Equal("subquery", items[17]!.Kind);
        Assert.Equal(["a"], items[18]!.Over!.PartitionBy!.Select(p => p.Parts![0]));
        Assert.True(items[18]!.Over!.OrderBy![0].Descending);
        Assert.Contains("UNBOUNDED PRECEDING", items[18]!.Over!.Frame);
        Assert.False(items[18]!.Aggregate);
        Assert.Equal("CURRENT_TIMESTAMP", items[19]!.Name);
        Assert.Equal("LEFT", items[20]!.Name);
        Assert.Null(items[21]);  // t.* is a star item
    }

    [Fact]
    public void SpansPointAtTheSourceTextAcrossBatches()
    {
        var sql = "SELECT 1;\nGO\n-- 🐘 comment\nSELECT o.Name\nFROM dbo.Orders AS o WHERE o.Id = 5";
        var r = ScriptParser.Parse(sql, ScriptParser.DefaultCompatLevel, model: true);
        var st = r.Batches[1].Statements![0];
        Assert.Equal(4, st.Span.Line);
        Assert.StartsWith("SELECT o.Name", Text(sql, st.Span));
        var where = st.Query!.Where!;
        Assert.Equal("o.Id = 5", Text(sql, where.Span));
        Assert.Equal(5, where.Span.Line);
        Assert.Equal("dbo.Orders AS o", Text(sql, st.Query.From!.Span));
    }

    [Fact]
    public void UnmodelledConstructsAreUnsupportedNotDropped()
    {
        var q = Select("SELECT a FROM OPENJSON(@j) WITH (a int '$.a') AS j WHERE CONTAINS(name, 'x')");
        Assert.Equal("unsupported", q.From!.Kind);
        Assert.Equal("OpenJsonTableReference", q.From.Type);
        Assert.Equal("unsupported", q.Where!.Kind);
        Assert.NotNull(q.Where.Type);
    }

    [Fact]
    public void BatchesWithSyntaxErrorsHaveNoModel()
    {
        var r = ScriptParser.Parse("SELECT 1\nGO\nSELECT FROM", ScriptParser.DefaultCompatLevel, model: true);
        Assert.NotNull(r.Batches[0].Statements);
        Assert.Null(r.Batches[1].Statements);
        Assert.Null(ScriptParser.Parse("SELECT 1", ScriptParser.DefaultCompatLevel).Batches[0].Statements);
    }

    [Fact]
    public void VeryDeepExpressionsAreCutOff()
    {
        var sql = "SELECT 1 WHERE " + string.Join(" AND ", Enumerable.Range(0, ModelBuilder.MaxDepth + 50).Select(i => $"a{i} = {i}"));
        var where = Select(sql).Where!;
        var depth = 0;
        var e = where;
        while (e.Kind == "and")
        {
            depth++;
            e = e.Left!;
        }
        Assert.Equal("unsupported", e.Kind);
        Assert.Equal("TooDeep", e.Type);
        Assert.Equal(ModelBuilder.MaxDepth, depth);
    }

    [Fact]
    public void JsonOmitsAbsentValuesAndFalseFlags()
    {
        var output = new StringWriter();
        var req = JsonSerializer.Serialize(new Dictionary<string, object> { ["id"] = "1", ["op"] = "parse", ["sql"] = "SELECT a FROM t", ["model"] = true });
        new Server(new StringReader(req), output, TextWriter.Null).Run();
        var json = output.ToString().Split('\n')[1];
        Assert.Contains("\"modelVersion\":1", json);
        Assert.Contains("\"statements\":[", json);
        Assert.DoesNotContain("null", json);
        Assert.DoesNotContain(":false", json);
    }

    // ---- The sample queries (tests/sample_queries): the model must capture each bug ----------

    private static Query SampleSelect(string file) => Assert.Single(Model(SampleQuery(file))).Query!;

    [Fact]
    public void Sample01InnerVersusLeftJoin()
    {
        Assert.Equal("inner", SampleSelect("01_employees_without_department.buggy.sql").From!.JoinType);
        Assert.Equal("left", SampleSelect("01_employees_without_department.correct.sql").From!.JoinType);
    }

    [Fact]
    public void Sample02FanOutAndSeparateAggregates()
    {
        var buggy = SampleSelect("02_one_to_many_fanout.buggy.sql");
        Assert.Equal(["inner", "inner", "inner"], JoinTypes(buggy.From!));
        Assert.True(buggy.Select!.Where(s => s.Expr!.Kind == "function").All(s => s.Expr!.Aggregate));
        var st = Assert.Single(Model(SampleQuery("02_one_to_many_fanout.correct.sql")));
        Assert.Equal(["paid", "items"], st.Ctes!.Select(c => c.Name));
        Assert.All(new[] { st.Query!.From!.Left!.Right!, st.Query.From.Right! }, s => Assert.True(s.Cte));
    }

    [Fact]
    public void Sample03CountDistinctOfNormalisedEmail()
    {
        var count = SampleSelect("03_duplicate_customers.correct.sql").Select![0].Expr!;
        Assert.True(count.Aggregate && count.Distinct);
        Assert.Equal("COALESCE", count.Args![0].Name);
    }

    [Fact]
    public void Sample04NotInWithSubquery()
    {
        var w = SampleSelect("04_not_in_with_null.buggy.sql").Where!;
        Assert.Equal(("in", true), (w.Kind, w.Not));
        Assert.Equal("Orders", w.Query!.From!.Name!.Name);
        var fixedW = SampleSelect("04_not_in_with_null.correct.sql").Where!;
        Assert.Equal("not", fixedW.Kind);
        Assert.Equal("exists", fixedW.Operand!.Kind);
    }

    [Fact]
    public void Sample05BetweenVersusHalfOpenRange()
    {
        Assert.Equal("between", SampleSelect("05_date_boundary.buggy.sql").Where!.Kind);
        var w = SampleSelect("05_date_boundary.correct.sql").Where!;
        Assert.Equal(("and", ">=", "<"), (w.Kind, w.Left!.Op, w.Right!.Op));
    }

    [Fact]
    public void Sample06AndBindsTighterThanOr()
    {
        var w = SampleSelect("06_and_or_precedence.buggy.sql").Where!;
        Assert.Equal("or", w.Kind);
        Assert.Equal("and", w.Right!.Kind);
        Assert.False(w.Right.Parenthesized);
        var fixedW = SampleSelect("06_and_or_precedence.correct.sql").Where!;
        Assert.Equal(("and", "in"), (fixedW.Kind, fixedW.Left!.Kind));
    }

    [Fact]
    public void Sample07CountVersusCountDistinct()
    {
        var buggy = SampleSelect("07_count_vs_count_distinct.buggy.sql").Select![1].Expr!;
        var correct = SampleSelect("07_count_vs_count_distinct.correct.sql").Select![1].Expr!;
        Assert.Equal(("COUNT", false), (buggy.Name, buggy.Distinct));
        Assert.Equal(("COUNT", true), (correct.Name, correct.Distinct));
    }

    [Fact]
    public void Sample08JoinOnTheWrongKey()
    {
        var on = SampleSelect("08_wrong_join_key.buggy.sql").From!.On!;
        Assert.Equal(["p", "ProductID"], on.Left!.Parts!);
        Assert.Equal(["oi", "OrderItemID"], on.Right!.Parts!);
    }

    [Fact]
    public void Sample09AverageOverLinesVersusOrders()
    {
        Assert.Equal("table", SampleSelect("09_aggregation_granularity.buggy.sql").From!.Kind);
        var correct = SampleSelect("09_aggregation_granularity.correct.sql");
        Assert.Equal("derived", correct.From!.Kind);
        Assert.Single(correct.From.Query!.GroupBy!);
    }

    [Fact]
    public void Sample10CommaJoinWithoutPredicate()
    {
        var from = SampleSelect("10_missing_join_predicate.buggy.sql").From!;
        Assert.Equal("comma", from.JoinType);
        Assert.Null(from.On);
        var fixedFrom = SampleSelect("10_missing_join_predicate.correct.sql").From!;
        Assert.Equal("inner", fixedFrom.JoinType);
        Assert.NotNull(fixedFrom.On);
    }

    [Fact]
    public void EverySampleQueryProducesAModel()
    {
        var dir = Path.GetDirectoryName(Path.GetFullPath(Path.Combine(AppContext.BaseDirectory)))!;
        while (!Directory.Exists(Path.Combine(dir, "tests", "sample_queries")))
        {
            dir = Path.GetDirectoryName(dir)!;
        }
        var files = Directory.GetFiles(Path.Combine(dir, "tests", "sample_queries"), "*.sql");
        Assert.Equal(20, files.Length);
        foreach (var file in files)
        {
            var st = Assert.Single(Model(File.ReadAllText(file)));
            Assert.Equal("select", st.Kind);
            Assert.NotNull(st.Query);
        }
    }

    private static List<string> JoinTypes(Source s)
    {
        var list = new List<string>();
        for (var j = s; j is { Kind: "join" }; j = j.Left)
        {
            list.Insert(0, j.JoinType!);
        }
        return list;
    }
}
