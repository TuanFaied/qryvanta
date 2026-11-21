using Microsoft.SqlServer.TransactSql.ScriptDom;

namespace Qryvanta.TsqlParser;

/// <summary>
/// Converts one parsed batch (ScriptDom fragments) into QueryModel statements. Positions in
/// ScriptDom are relative to the batch text; spans are made script-absolute here.
/// </summary>
internal sealed class ModelBuilder(string batchText, int batchOffset, LineIndex lines)
{
    /// <summary>Deeper expression trees are cut off with an "unsupported" node instead of risking the stack.</summary>
    public const int MaxDepth = 256;

    private int _depth;
    // CTE names visible at the current point (innermost scope last).
    private readonly List<HashSet<string>> _cteScopes = [];

    public List<Statement> Statements(TSqlScript script)
    {
        var list = new List<Statement>();
        foreach (var batch in script.Batches)
        {
            foreach (var s in batch.Statements)
            {
                list.Add(Statement(s));
            }
        }
        return list;
    }

    private Span SpanOf(TSqlFragment f)
    {
        var offset = batchOffset + Math.Max(f.StartOffset, 0);
        var (line, column) = lines.Position(offset);
        return new Span(offset, Math.Max(f.FragmentLength, 0), line, column);
    }

    private string TextOf(TSqlFragment f) =>
        f.StartOffset >= 0 && f.StartOffset + f.FragmentLength <= batchText.Length
            ? batchText.Substring(f.StartOffset, f.FragmentLength)
            : "";

    private static List<T>? NonEmpty<T>(List<T> list) => list.Count == 0 ? null : list;

    private static ObjectName? Name(SchemaObjectName? n) => n is null ? null : new ObjectName
    {
        Server = n.ServerIdentifier?.Value,
        Database = n.DatabaseIdentifier?.Value,
        Schema = n.SchemaIdentifier?.Value,
        Name = n.BaseIdentifier?.Value ?? "",
    };

    // ---- Statements -------------------------------------------------------------------------

    private Statement Statement(TSqlStatement s)
    {
        var st = new Statement { Type = s.GetType().Name, Span = SpanOf(s) };
        switch (s)
        {
            case SelectStatement sel:
                st.Kind = "select";
                WithCtes(st, sel.WithCtesAndXmlNamespaces, () =>
                {
                    st.Query = Query(sel.QueryExpression);
                    st.Into = Name(sel.Into);
                });
                break;
            case InsertStatement ins:
                st.Kind = "insert";
                WithCtes(st, ins.WithCtesAndXmlNamespaces, () =>
                {
                    st.Target = Source(ins.InsertSpecification.Target);
                    if (ins.InsertSpecification.InsertSource is SelectInsertSource sis)
                    {
                        st.Query = Query(sis.Select);
                    }
                });
                break;
            case UpdateStatement upd:
                st.Kind = "update";
                WithCtes(st, upd.WithCtesAndXmlNamespaces, () =>
                {
                    st.Target = Source(upd.UpdateSpecification.Target);
                    st.From = FromClause(upd.UpdateSpecification.FromClause);
                    st.Where = Bool(upd.UpdateSpecification.WhereClause?.SearchCondition);
                });
                break;
            case DeleteStatement del:
                st.Kind = "delete";
                WithCtes(st, del.WithCtesAndXmlNamespaces, () =>
                {
                    st.Target = Source(del.DeleteSpecification.Target);
                    st.From = FromClause(del.DeleteSpecification.FromClause);
                    st.Where = Bool(del.DeleteSpecification.WhereClause?.SearchCondition);
                });
                break;
            case MergeStatement merge:
                st.Kind = "merge";
                WithCtes(st, merge.WithCtesAndXmlNamespaces, () => st.Target = Source(merge.MergeSpecification.Target));
                break;
            case TruncateTableStatement trunc:
                st.Kind = "truncate";
                st.Target = new Source { Kind = "table", Span = SpanOf(trunc.TableName), Name = Name(trunc.TableName) };
                break;
            case ExecuteStatement exec:
                st.Kind = "exec";
                if (exec.ExecuteSpecification?.ExecutableEntity is ExecutableProcedureReference proc)
                {
                    st.Procedure = Name(proc.ProcedureReference?.ProcedureReference?.Name);
                }
                break;
            case DeclareVariableStatement or DeclareTableVariableStatement or DeclareCursorStatement:
                st.Kind = "declare";
                break;
            case SetVariableStatement:
                st.Kind = "set";
                break;
            case PredicateSetStatement or SetOnOffStatement or SetCommandStatement or SetRowCountStatement
                or SetTransactionIsolationLevelStatement or SetTextSizeStatement or SetErrorLevelStatement:
                st.Kind = "setOption";
                st.Options = SetOptionNames(s);
                break;
            case IfStatement ifs:
                st.Kind = "if";
                st.Condition = Bool(ifs.Predicate);
                st.Body = [Statement(ifs.ThenStatement)];
                st.Else = ifs.ElseStatement is null ? null : [Statement(ifs.ElseStatement)];
                break;
            case WhileStatement wh:
                st.Kind = "while";
                st.Condition = Bool(wh.Predicate);
                st.Body = wh.Statement is null ? null : [Statement(wh.Statement)];
                break;
            case BeginEndBlockStatement block:
                st.Kind = "block";
                st.Body = NonEmpty(block.StatementList.Statements.Select(Statement).ToList());
                break;
            case TryCatchStatement tc:
                st.Kind = "try";
                st.Body = NonEmpty(tc.TryStatements.Statements.Select(Statement).ToList());
                st.Else = NonEmpty(tc.CatchStatements.Statements.Select(Statement).ToList());
                break;
            case BeginTransactionStatement:
                st.Kind = "transaction";
                st.Action = "begin";
                break;
            case CommitTransactionStatement:
                st.Kind = "transaction";
                st.Action = "commit";
                break;
            case RollbackTransactionStatement:
                st.Kind = "transaction";
                st.Action = "rollback";
                break;
            case SaveTransactionStatement:
                st.Kind = "transaction";
                st.Action = "save";
                break;
            case UseStatement:
                st.Kind = "use";
                break;
            case PrintStatement:
                st.Kind = "print";
                break;
            case WaitForStatement wf:
                st.Kind = "waitfor";
                st.Action = wf.WaitForOption.ToString().ToLowerInvariant();
                break;
            case ReturnStatement:
                st.Kind = "return";
                break;
            default:
                st.Kind = IsDdl(s) ? "ddl" : "other";
                break;
        }
        st.Effects = NonEmpty(new EffectCollector(this).Collect(s));
        return st;
    }

    /// <summary>The options a SET statement changes, as ScriptDom's enum names.</summary>
    private static List<string>? SetOptionNames(TSqlStatement s)
    {
        static IEnumerable<string> Flags(Enum e) =>
            e.ToString().Split(',', StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries);
        IEnumerable<string> names = s switch
        {
            PredicateSetStatement p => Flags(p.Options),
            SetStatisticsStatement st => Flags(st.Options),
            SetOffsetsStatement o => Flags(o.Options),
            SetCommandStatement c => c.Commands.Select(cmd => cmd is GeneralSetCommand g ? g.CommandType.ToString() : cmd.GetType().Name),
            SetTransactionIsolationLevelStatement t => [t.Level.ToString()],
            _ => [],
        };
        return NonEmpty(names.ToList());
    }

    /// <summary>
    /// Collects a statement's effects by visiting its whole syntax tree, so a write or EXEC
    /// hidden in a construct the model does not describe is still reported.
    /// </summary>
    private sealed class EffectCollector(ModelBuilder b) : TSqlFragmentVisitor
    {
        private readonly List<Effect> _effects = [];

        public List<Effect> Collect(TSqlStatement s)
        {
            s.Accept(this);
            return _effects;
        }

        private void Write(TSqlFragment at, ObjectName? obj, string? variable = null) =>
            _effects.Add(new Effect { Kind = "write", Object = obj, Variable = variable, Span = b.SpanOf(at) });

        /// <summary>A DML target. A single-part name that is an alias in FROM resolves to that table.</summary>
        private void Target(TableReference? target, FromClause? from)
        {
            switch (target)
            {
                case NamedTableReference named:
                    Write(named, Name(ResolveAlias(named.SchemaObject, from)));
                    break;
                case VariableTableReference v:
                    Write(v, null, v.Variable?.Name);
                    break;
                case null:
                    break;
                default:
                    Write(target, null);
                    break;
            }
        }

        private static SchemaObjectName ResolveAlias(SchemaObjectName name, FromClause? from)
        {
            if (from is null || name.Count != 1) return name;
            var alias = name.BaseIdentifier?.Value;
            var match = from.TableReferences.SelectMany(Tables)
                .FirstOrDefault(t => string.Equals(t.Alias?.Value, alias, StringComparison.OrdinalIgnoreCase));
            return match?.SchemaObject ?? name;
        }

        private static IEnumerable<NamedTableReference> Tables(TableReference r) => r switch
        {
            NamedTableReference n => [n],
            QualifiedJoin j => Tables(j.FirstTableReference).Concat(Tables(j.SecondTableReference)),
            UnqualifiedJoin j => Tables(j.FirstTableReference).Concat(Tables(j.SecondTableReference)),
            JoinParenthesisTableReference p => Tables(p.Join),
            _ => [],
        };

        public override void Visit(InsertSpecification n) => Target(n.Target, null);
        public override void Visit(UpdateSpecification n) => Target(n.Target, n.FromClause);
        public override void Visit(DeleteSpecification n) => Target(n.Target, n.FromClause);
        public override void Visit(MergeSpecification n) => Target(n.Target, null);
        public override void Visit(OutputIntoClause n) => Target(n.IntoTable, null);

        public override void Visit(SelectStatement n)
        {
            if (n.Into is not null) Write(n.Into, Name(n.Into));
        }

        public override void Visit(TruncateTableStatement n) => Write(n.TableName, Name(n.TableName));
        public override void Visit(CreateTableStatement n) => Write(n.SchemaObjectName, Name(n.SchemaObjectName));
        public override void Visit(AlterTableStatement n) => Write(n.SchemaObjectName, Name(n.SchemaObjectName));
        public override void Visit(CreateIndexStatement n) => Write(n.OnName, Name(n.OnName));

        public override void Visit(DropObjectsStatement n)
        {
            foreach (var o in n.Objects) Write(o, Name(o));
        }

        public override void Visit(ExecuteSpecification n)
        {
            var proc = (n.ExecutableEntity as ExecutableProcedureReference)?.ProcedureReference?.ProcedureReference?.Name;
            _effects.Add(new Effect { Kind = "exec", Object = Name(proc), Span = b.SpanOf(n) });
        }

        private void Remote(TSqlFragment n) =>
            _effects.Add(new Effect { Kind = "remote", Type = n.GetType().Name, Span = b.SpanOf(n) });

        public override void Visit(OpenRowsetTableReference n) => Remote(n);
        public override void Visit(OpenQueryTableReference n) => Remote(n);
        public override void Visit(AdHocTableReference n) => Remote(n);
        public override void Visit(BulkOpenRowset n) => Remote(n);
        public override void Visit(InternalOpenRowset n) => Remote(n);
    }

    private static bool IsDdl(TSqlStatement s)
    {
        var name = s.GetType().Name;
        return name.StartsWith("Create", StringComparison.Ordinal)
            || name.StartsWith("Alter", StringComparison.Ordinal)
            || name.StartsWith("Drop", StringComparison.Ordinal);
    }

    /// <summary>Records a statement's WITH clause and makes its names visible while building the rest.</summary>
    private void WithCtes(Statement st, WithCtesAndXmlNamespaces? with, Action build)
    {
        var ctes = with?.CommonTableExpressions;
        if (ctes is null || ctes.Count == 0)
        {
            build();
            return;
        }
        var names = new HashSet<string>(StringComparer.OrdinalIgnoreCase);
        _cteScopes.Add(names);
        try
        {
            var list = new List<Cte>();
            foreach (var c in ctes)
            {
                // A CTE sees itself (recursion) and the ones declared before it.
                names.Add(c.ExpressionName.Value);
                var query = Query(c.QueryExpression);
                list.Add(new Cte
                {
                    Name = c.ExpressionName.Value,
                    Columns = NonEmpty(c.Columns.Select(i => i.Value).ToList()),
                    Query = query,
                    Recursive = References(query, c.ExpressionName.Value),
                    Span = SpanOf(c),
                });
            }
            st.Ctes = list;
            build();
        }
        finally
        {
            _cteScopes.RemoveAt(_cteScopes.Count - 1);
        }
    }

    private bool IsCte(SchemaObjectName n) =>
        n.SchemaIdentifier is null && n.DatabaseIdentifier is null && n.BaseIdentifier is not null
        && _cteScopes.Any(scope => scope.Contains(n.BaseIdentifier.Value));

    private static bool References(Query q, string cte)
    {
        static bool InSource(Source? s, string name) => s switch
        {
            null => false,
            { Kind: "table", Cte: true } => string.Equals(s.Name?.Name, name, StringComparison.OrdinalIgnoreCase),
            { Kind: "join" } => InSource(s.Left, name) || InSource(s.Right, name),
            { Kind: "derived" } => s.Query is not null && References(s.Query, name),
            _ => false,
        };
        return q.Kind == "setOp"
            ? (q.Left is not null && References(q.Left, cte)) || (q.Right is not null && References(q.Right, cte))
            : InSource(q.From, cte);
    }

    // ---- Queries ------------------------------------------------------------------------------

    private Query Query(QueryExpression q)
    {
        var parenthesized = false;
        while (q is QueryParenthesisExpression p)
        {
            parenthesized = true;
            q = p.QueryExpression;
        }

        Query result;
        switch (q)
        {
            case QuerySpecification spec:
                result = new Query
                {
                    Kind = "select",
                    Distinct = spec.UniqueRowFilter == UniqueRowFilter.Distinct,
                    Top = spec.TopRowFilter is null ? null : new Top
                    {
                        Expr = Scalar(spec.TopRowFilter.Expression),
                        Percent = spec.TopRowFilter.Percent,
                        WithTies = spec.TopRowFilter.WithTies,
                    },
                    Select = NonEmpty(spec.SelectElements.Select(SelectItem).ToList()),
                    From = FromClause(spec.FromClause),
                    Where = Bool(spec.WhereClause?.SearchCondition),
                    GroupBy = spec.GroupByClause is null ? null : NonEmpty(spec.GroupByClause.GroupingSpecifications
                        .Select(g => g is ExpressionGroupingSpecification e ? Scalar(e.Expression) : Unsupported(g)).ToList()),
                    Having = Bool(spec.HavingClause?.SearchCondition),
                };
                break;
            case BinaryQueryExpression bin:
                result = SetOperation(bin);
                break;
            default:
                result = new Query { Kind = "unsupported" };
                break;
        }
        result.Span = SpanOf(q);
        result.Parenthesized = parenthesized;
        result.OrderBy = OrderBy(q.OrderByClause);
        result.Offset = q.OffsetClause is null ? null : Scalar(q.OffsetClause.OffsetExpression);
        result.Fetch = q.OffsetClause?.FetchExpression is null ? null : Scalar(q.OffsetClause.FetchExpression);
        return result;
    }

    /// <summary>
    /// Rebuilds a chain of set operations with SQL Server's precedence. ScriptDom nests them
    /// strictly left to right, so "a UNION b INTERSECT c" arrives as (a UNION b) INTERSECT c,
    /// but SQL Server evaluates INTERSECT first: a UNION (b INTERSECT c). UNION, UNION ALL and
    /// EXCEPT have equal precedence and associate left to right. A parenthesized operand stays
    /// one unit.
    /// </summary>
    private Query SetOperation(BinaryQueryExpression top)
    {
        var operands = new List<Query>();
        var ops = new List<string>();
        void Flatten(QueryExpression e)
        {
            if (e is BinaryQueryExpression b)
            {
                Flatten(b.FirstQueryExpression);
                ops.Add(b.BinaryQueryExpressionType switch
                {
                    BinaryQueryExpressionType.Union => b.All ? "unionAll" : "union",
                    BinaryQueryExpressionType.Except => "except",
                    BinaryQueryExpressionType.Intersect => "intersect",
                    _ => b.BinaryQueryExpressionType.ToString().ToLowerInvariant(),
                });
                Flatten(b.SecondQueryExpression);
            }
            else
            {
                operands.Add(Query(e));
            }
        }
        Flatten(top);

        static Query Node(string op, Query left, Query right) => new()
        {
            Kind = "setOp",
            Op = op,
            Left = left,
            Right = right,
            Span = new Span(left.Span.Offset, right.Span.Offset + right.Span.Length - left.Span.Offset, left.Span.Line, left.Span.Column),
        };

        // INTERSECT first, then the rest left to right.
        var terms = new List<Query> { operands[0] };
        var termOps = new List<string>();
        for (var i = 0; i < ops.Count; i++)
        {
            if (ops[i] == "intersect")
            {
                terms[^1] = Node("intersect", terms[^1], operands[i + 1]);
            }
            else
            {
                termOps.Add(ops[i]);
                terms.Add(operands[i + 1]);
            }
        }
        var result = terms[0];
        for (var i = 0; i < termOps.Count; i++)
        {
            result = Node(termOps[i], result, terms[i + 1]);
        }
        return result;
    }

    private List<OrderItem>? OrderBy(OrderByClause? clause) =>
        clause is null ? null : NonEmpty(clause.OrderByElements.Select(e => new OrderItem
        {
            Expr = Scalar(e.Expression),
            Descending = e.SortOrder == SortOrder.Descending,
        }).ToList());

    private SelectItem SelectItem(SelectElement e) => e switch
    {
        SelectScalarExpression s => new SelectItem
        {
            Kind = "expr",
            Expr = Scalar(s.Expression),
            Alias = s.ColumnName?.Value,
            Span = SpanOf(s),
        },
        SelectStarExpression star => new SelectItem
        {
            Kind = "star",
            Qualifier = star.Qualifier is null ? null : NonEmpty(star.Qualifier.Identifiers.Select(i => i.Value).ToList()),
            Span = SpanOf(star),
        },
        _ => new SelectItem { Kind = "unsupported", Span = SpanOf(e) },
    };

    // ---- Sources ------------------------------------------------------------------------------

    /// <summary>The FROM list as one tree; "FROM a, b, c" becomes ((a comma b) comma c).</summary>
    private Source? FromClause(FromClause? from)
    {
        if (from is null || from.TableReferences.Count == 0)
        {
            return null;
        }
        var result = Source(from.TableReferences[0]);
        for (var i = 1; i < from.TableReferences.Count; i++)
        {
            var right = Source(from.TableReferences[i]);
            result = new Source
            {
                Kind = "join",
                JoinType = "comma",
                Left = result,
                Right = right,
                Span = new Span(result.Span.Offset, right.Span.Offset + right.Span.Length - result.Span.Offset, result.Span.Line, result.Span.Column),
            };
        }
        return result;
    }

    private Source Source(TableReference t)
    {
        var parenthesized = false;
        while (t is JoinParenthesisTableReference p)
        {
            parenthesized = true;
            t = p.Join;
        }

        var s = new Source { Span = SpanOf(t), Parenthesized = parenthesized };
        switch (t)
        {
            case NamedTableReference n:
                s.Kind = "table";
                s.Name = Name(n.SchemaObject);
                s.Cte = IsCte(n.SchemaObject);
                s.Alias = n.Alias?.Value;
                break;
            case QueryDerivedTable d:
                s.Kind = "derived";
                s.Query = Query(d.QueryExpression);
                s.Alias = d.Alias?.Value;
                s.Columns = NonEmpty(d.Columns.Select(c => c.Value).ToList());
                break;
            case QualifiedJoin qj:
                s.Kind = "join";
                s.JoinType = qj.QualifiedJoinType switch
                {
                    QualifiedJoinType.Inner => "inner",
                    QualifiedJoinType.LeftOuter => "left",
                    QualifiedJoinType.RightOuter => "right",
                    QualifiedJoinType.FullOuter => "full",
                    _ => qj.QualifiedJoinType.ToString().ToLowerInvariant(),
                };
                s.Left = Source(qj.FirstTableReference);
                s.Right = Source(qj.SecondTableReference);
                s.On = Bool(qj.SearchCondition);
                break;
            case UnqualifiedJoin uj:
                s.Kind = "join";
                s.JoinType = uj.UnqualifiedJoinType switch
                {
                    UnqualifiedJoinType.CrossJoin => "cross",
                    UnqualifiedJoinType.CrossApply => "crossApply",
                    UnqualifiedJoinType.OuterApply => "outerApply",
                    _ => uj.UnqualifiedJoinType.ToString().ToLowerInvariant(),
                };
                s.Left = Source(uj.FirstTableReference);
                s.Right = Source(uj.SecondTableReference);
                break;
            case SchemaObjectFunctionTableReference f:
                s.Kind = "function";
                s.Name = Name(f.SchemaObject);
                s.Args = NonEmpty(f.Parameters.Select(Scalar).ToList());
                s.Alias = f.Alias?.Value;
                break;
            case GlobalFunctionTableReference g:
                s.Kind = "function";
                s.Name = new ObjectName { Name = g.Name.Value };
                s.Args = NonEmpty(g.Parameters.Select(Scalar).ToList());
                s.Alias = g.Alias?.Value;
                break;
            case VariableTableReference v:
                s.Kind = "variable";
                s.Variable = v.Variable.Name;
                s.Alias = v.Alias?.Value;
                break;
            case InlineDerivedTable values:
                s.Kind = "values";
                s.Alias = values.Alias?.Value;
                s.Columns = NonEmpty(values.Columns.Select(c => c.Value).ToList());
                break;
            case TableReferenceWithAlias other:
                s.Kind = "unsupported";
                s.Type = other.GetType().Name;
                s.Alias = other.Alias?.Value;
                break;
            default:
                s.Kind = "unsupported";
                s.Type = t.GetType().Name;
                break;
        }
        return s;
    }

    // ---- Expressions --------------------------------------------------------------------------

    private Expr Unsupported(TSqlFragment f) => new() { Kind = "unsupported", Type = f.GetType().Name, Span = SpanOf(f) };

    private Expr? Bool(BooleanExpression? b) => b is null ? null : Guard(b, () => BoolCore(b));

    private Expr Guard(TSqlFragment f, Func<Expr> build)
    {
        if (_depth >= MaxDepth)
        {
            return new Expr { Kind = "unsupported", Type = "TooDeep", Span = SpanOf(f) };
        }
        _depth++;
        try
        {
            return build();
        }
        finally
        {
            _depth--;
        }
    }

    private Expr BoolCore(BooleanExpression b)
    {
        var parenthesized = false;
        while (b is BooleanParenthesisExpression p)
        {
            parenthesized = true;
            b = p.Expression;
        }
        var e = new Expr { Span = SpanOf(b), Parenthesized = parenthesized };
        switch (b)
        {
            case BooleanBinaryExpression bin:
                e.Kind = bin.BinaryExpressionType == BooleanBinaryExpressionType.And ? "and" : "or";
                e.Left = Bool(bin.FirstExpression);
                e.Right = Bool(bin.SecondExpression);
                break;
            case BooleanNotExpression not:
                e.Kind = "not";
                e.Operand = Bool(not.Expression);
                break;
            case BooleanComparisonExpression cmp:
                e.Kind = "compare";
                e.Op = CompareOp(cmp.ComparisonType);
                e.Left = Scalar(cmp.FirstExpression);
                e.Right = Scalar(cmp.SecondExpression);
                break;
            case SubqueryComparisonPredicate sub:
                e.Kind = "compare";
                e.Op = CompareOp(sub.ComparisonType);
                e.Quantifier = sub.SubqueryComparisonPredicateType == SubqueryComparisonPredicateType.All ? "all" : "any";
                e.Left = Scalar(sub.Expression);
                e.Query = Query(sub.Subquery.QueryExpression);
                break;
            case BooleanIsNullExpression isNull:
                e.Kind = "isNull";
                e.Not = isNull.IsNot;
                e.Operand = Scalar(isNull.Expression);
                break;
            case InPredicate inp:
                e.Kind = "in";
                e.Not = inp.NotDefined;
                e.Operand = Scalar(inp.Expression);
                if (inp.Subquery is not null)
                {
                    e.Query = Query(inp.Subquery.QueryExpression);
                }
                else
                {
                    e.Values = NonEmpty(inp.Values.Select(Scalar).ToList());
                }
                break;
            case BooleanTernaryExpression tern:
                e.Kind = "between";
                e.Not = tern.TernaryExpressionType == BooleanTernaryExpressionType.NotBetween;
                e.Operand = Scalar(tern.FirstExpression);
                e.Low = Scalar(tern.SecondExpression);
                e.High = Scalar(tern.ThirdExpression);
                break;
            case LikePredicate like:
                e.Kind = "like";
                e.Not = like.NotDefined;
                e.Operand = Scalar(like.FirstExpression);
                e.Pattern = Scalar(like.SecondExpression);
                e.Escape = like.EscapeExpression is null ? null : Scalar(like.EscapeExpression);
                break;
            case ExistsPredicate exists:
                e.Kind = "exists";
                e.Query = Query(exists.Subquery.QueryExpression);
                break;
            default:
                e.Kind = "unsupported";
                e.Type = b.GetType().Name;
                break;
        }
        return e;
    }

    private static string CompareOp(BooleanComparisonType t) => t switch
    {
        BooleanComparisonType.Equals => "=",
        BooleanComparisonType.NotEqualToBrackets => "<>",
        BooleanComparisonType.NotEqualToExclamation => "!=",
        BooleanComparisonType.GreaterThan => ">",
        BooleanComparisonType.LessThan => "<",
        BooleanComparisonType.GreaterThanOrEqualTo => ">=",
        BooleanComparisonType.LessThanOrEqualTo => "<=",
        BooleanComparisonType.NotLessThan => "!<",
        BooleanComparisonType.NotGreaterThan => "!>",
        _ => t.ToString(),
    };

    private Expr Scalar(ScalarExpression s) => Guard(s, () => ScalarCore(s));

    private static readonly HashSet<string> AggregateNames = new(StringComparer.OrdinalIgnoreCase)
    {
        "AVG", "CHECKSUM_AGG", "COUNT", "COUNT_BIG", "GROUPING", "GROUPING_ID", "MAX", "MIN", "STDEV",
        "STDEVP", "STRING_AGG", "SUM", "VAR", "VARP", "APPROX_COUNT_DISTINCT", "APPROX_PERCENTILE_CONT",
        "APPROX_PERCENTILE_DISC",
    };

    private Expr ScalarCore(ScalarExpression s)
    {
        var parenthesized = false;
        while (s is ParenthesisExpression p)
        {
            parenthesized = true;
            s = p.Expression;
        }
        var e = new Expr { Span = SpanOf(s), Parenthesized = parenthesized };
        switch (s)
        {
            case ColumnReferenceExpression col:
                e.Kind = "column";
                var parts = col.MultiPartIdentifier?.Identifiers.Select(i => i.Value).ToList() ?? [];
                if (col.ColumnType == ColumnType.Wildcard)
                {
                    parts.Add("*");
                }
                e.Parts = parts;
                break;
            case StringLiteral str:
                e.Kind = "literal";
                e.LiteralType = "string";
                e.Value = str.Value;
                e.National = str.IsNational;
                break;
            case NullLiteral:
                e.Kind = "literal";
                e.LiteralType = "null";
                break;
            case Literal lit:
                e.Kind = "literal";
                e.LiteralType = lit.LiteralType switch
                {
                    LiteralType.Integer => "integer",
                    LiteralType.Numeric => "numeric",
                    LiteralType.Real => "real",
                    LiteralType.Money => "money",
                    LiteralType.Binary => "binary",
                    LiteralType.Default => "default",
                    LiteralType.Max => "max",
                    _ => lit.LiteralType.ToString().ToLowerInvariant(),
                };
                e.Value = lit.Value;
                break;
            case VariableReference v:
                e.Kind = "variable";
                e.Name = v.Name;
                break;
            case GlobalVariableExpression g:
                e.Kind = "variable";
                e.Name = g.Name;
                break;
            case FunctionCall f:
                e.Kind = "function";
                e.Name = f.FunctionName.Value;
                e.Schema = f.CallTarget is MultiPartIdentifierCallTarget target
                    ? string.Join(".", target.MultiPartIdentifier.Identifiers.Select(i => i.Value))
                    : null;
                e.Args = NonEmpty(f.Parameters.Select(Scalar).ToList());
                e.Distinct = f.UniqueRowFilter == UniqueRowFilter.Distinct;
                e.Aggregate = e.Schema is null && AggregateNames.Contains(e.Name);
                if (f.OverClause is { } over)
                {
                    e.Over = new Over
                    {
                        PartitionBy = NonEmpty(over.Partitions.Select(Scalar).ToList()),
                        OrderBy = OrderBy(over.OrderByClause),
                        Frame = over.WindowFrameClause is null ? null : TextOf(over.WindowFrameClause),
                    };
                }
                break;
            case CoalesceExpression co:
                Function(e, "COALESCE", co.Expressions);
                break;
            case NullIfExpression ni:
                Function(e, "NULLIF", [ni.FirstExpression, ni.SecondExpression]);
                break;
            case IIfCall iif:
                e.Kind = "function";
                e.Name = "IIF";
                e.Args = [Bool(iif.Predicate)!, Scalar(iif.ThenExpression), Scalar(iif.ElseExpression)];
                break;
            case LeftFunctionCall left:
                Function(e, "LEFT", left.Parameters);
                break;
            case RightFunctionCall right:
                Function(e, "RIGHT", right.Parameters);
                break;
            case ParameterlessCall pc:
                e.Kind = "function";
                e.Name = pc.ParameterlessCallType.ToString().ToUpperInvariant() switch
                {
                    "CURRENTTIMESTAMP" => "CURRENT_TIMESTAMP",
                    "CURRENTUSER" => "CURRENT_USER",
                    "SESSIONUSER" => "SESSION_USER",
                    "SYSTEMUSER" => "SYSTEM_USER",
                    "USER" => "USER",
                    var other => other,
                };
                break;
            case CastCall cast:
                Cast(e, "CAST", cast.DataType, cast.Parameter, null);
                break;
            case TryCastCall tryCast:
                Cast(e, "TRY_CAST", tryCast.DataType, tryCast.Parameter, null);
                break;
            case ConvertCall conv:
                Cast(e, "CONVERT", conv.DataType, conv.Parameter, conv.Style);
                break;
            case TryConvertCall tryConv:
                Cast(e, "TRY_CONVERT", tryConv.DataType, tryConv.Parameter, tryConv.Style);
                break;
            case ScalarSubquery sub:
                e.Kind = "subquery";
                e.Query = Query(sub.QueryExpression);
                break;
            case BinaryExpression bin:
                e.Kind = "arithmetic";
                e.Op = bin.BinaryExpressionType switch
                {
                    BinaryExpressionType.Add => "+",
                    BinaryExpressionType.Subtract => "-",
                    BinaryExpressionType.Multiply => "*",
                    BinaryExpressionType.Divide => "/",
                    BinaryExpressionType.Modulo => "%",
                    BinaryExpressionType.BitwiseAnd => "&",
                    BinaryExpressionType.BitwiseOr => "|",
                    BinaryExpressionType.BitwiseXor => "^",
                    _ => bin.BinaryExpressionType.ToString(),
                };
                e.Left = Scalar(bin.FirstExpression);
                e.Right = Scalar(bin.SecondExpression);
                break;
            case UnaryExpression un:
                e.Kind = "unary";
                e.Op = un.UnaryExpressionType switch
                {
                    UnaryExpressionType.Positive => "+",
                    UnaryExpressionType.Negative => "-",
                    UnaryExpressionType.BitwiseNot => "~",
                    _ => un.UnaryExpressionType.ToString(),
                };
                e.Operand = Scalar(un.Expression);
                break;
            case SearchedCaseExpression sc:
                e.Kind = "case";
                e.Whens = sc.WhenClauses.Select(w => new When { Condition = Bool(w.WhenExpression)!, Then = Scalar(w.ThenExpression) }).ToList();
                e.Else = sc.ElseExpression is null ? null : Scalar(sc.ElseExpression);
                break;
            case SimpleCaseExpression simple:
                e.Kind = "case";
                e.Operand = Scalar(simple.InputExpression);
                e.Whens = simple.WhenClauses.Select(w => new When { Condition = Scalar(w.WhenExpression), Then = Scalar(w.ThenExpression) }).ToList();
                e.Else = simple.ElseExpression is null ? null : Scalar(simple.ElseExpression);
                break;
            default:
                e.Kind = "unsupported";
                e.Type = s.GetType().Name;
                break;
        }
        return e;
    }

    private void Function(Expr e, string name, IEnumerable<ScalarExpression> args)
    {
        e.Kind = "function";
        e.Name = name;
        e.Args = NonEmpty(args.Select(Scalar).ToList());
    }

    private void Cast(Expr e, string name, DataTypeReference type, ScalarExpression operand, ScalarExpression? style)
    {
        e.Kind = "cast";
        e.Name = name;
        e.DataType = TextOf(type);
        e.Operand = Scalar(operand);
        e.Args = style is null ? null : [Scalar(style)];
    }
}
