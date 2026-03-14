package typesv1

func ExprLiteral(val *Val) *Expr {
	return &Expr{Expr: &Expr_Literal{Literal: val}}
}

func ExprUnary(op UnaryOp_Operator, arg *Expr) *Expr {
	return &Expr{Expr: &Expr_Unary{Unary: &UnaryOp{
		Op:  op,
		Arg: arg,
	}}}
}

func ExprBinary(lhs *Expr, op BinaryOp_Operator, rhs *Expr) *Expr {
	return &Expr{Expr: &Expr_Binary{Binary: &BinaryOp{
		Lhs: lhs,
		Op:  op,
		Rhs: rhs,
	}}}
}

func ExprFuncCall(name string, args ...*Expr) *Expr {
	return &Expr{Expr: &Expr_FuncCall{FuncCall: &FuncCall{
		Name: name,
		Args: args,
	}}}
}

func ExprIdentifier(id string) *Expr {
	return &Expr{Expr: &Expr_Identifier{Identifier: &Identifier{
		Name: id,
	}}}
}

func ExprIndexor(x *Expr, index *Expr) *Expr {
	return &Expr{Expr: &Expr_Indexor{Indexor: &Indexor{
		X:     x,
		Index: index,
	}}}
}
