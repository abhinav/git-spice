package main

type branchCommentCmd struct {
	List         branchCommentListCmd         `cmd:"" aliases:"ls" help:"List comments on a change request"`
	Stage        branchCommentStageCmd        `cmd:"" help:"Stage an inline comment for batch submission"`
	Add          branchCommentAddCmd          `cmd:"" help:"Post an inline comment immediately"`
	SubmitStaged branchCommentSubmitStagedCmd `cmd:"" aliases:"ss" help:"Submit all staged comments as a review"`
	Resolve      branchCommentResolveCmd      `cmd:"" help:"Resolve or unresolve a review thread"`
	Edit         branchCommentEditCmd         `cmd:"" help:"Edit a comment"`
}
