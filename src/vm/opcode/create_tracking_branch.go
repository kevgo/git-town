package opcode

import (
	"github.com/git-town/git-town/v11/src/git/gitdomain"
	"github.com/git-town/git-town/v11/src/vm/shared"
)

// CreateTrackingBranch pushes the given local branch up to origin
// and marks it as tracking the current branch.
type CreateTrackingBranch struct {
	Branch gitdomain.LocalBranchName
	undeclaredOpcodeMethods
}

func (self *CreateTrackingBranch) CreateContinueProgram() []shared.Opcode {
	return []shared.Opcode{
		self,
	}
}

func (self *CreateTrackingBranch) Run(args shared.RunArgs) error {
	return args.Runner.Frontend.CreateTrackingBranch(self.Branch, gitdomain.OriginRemote, args.Runner.NoPushHook())
}
