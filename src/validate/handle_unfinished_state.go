package validate

import (
	"fmt"

	"github.com/git-town/git-town/v11/src/cli/dialog"
	"github.com/git-town/git-town/v11/src/config/configdomain"
	"github.com/git-town/git-town/v11/src/git"
	"github.com/git-town/git-town/v11/src/git/gitdomain"
	"github.com/git-town/git-town/v11/src/hosting/hostingdomain"
	"github.com/git-town/git-town/v11/src/messages"
	"github.com/git-town/git-town/v11/src/undo/undoconfig"
	"github.com/git-town/git-town/v11/src/vm/interpreter"
	"github.com/git-town/git-town/v11/src/vm/runstate"
	"github.com/git-town/git-town/v11/src/vm/statefile"
)

// HandleUnfinishedState checks for unfinished state on disk, handles it, and signals whether to continue execution of the originally intended steps.
func HandleUnfinishedState(args UnfinishedStateArgs) (quit bool, err error) {
	runState, err := statefile.Load(args.RootDir)
	if err != nil {
		return false, fmt.Errorf(messages.RunstateLoadProblem, err)
	}
	if runState == nil || !runState.IsUnfinished() {
		return false, nil
	}
	response, err := dialog.AskHowToHandleUnfinishedRunState(
		runState.Command,
		runState.UnfinishedDetails.EndBranch,
		runState.UnfinishedDetails.EndTime,
		runState.UnfinishedDetails.CanSkip,
	)
	if err != nil {
		return quit, err
	}
	switch response {
	case dialog.ResponseDiscard:
		return discardRunstate(args.RootDir)
	case dialog.ResponseContinue:
		return continueRunstate(runState, args)
	case dialog.ResponseUndo:
		return abortRunstate(runState, args)
	case dialog.ResponseSkip:
		return skipRunstate(runState, args)
	case dialog.ResponseQuit:
		return true, nil
	default:
		return false, fmt.Errorf(messages.DialogUnexpectedResponse, response)
	}
}

type UnfinishedStateArgs struct {
	Connector               hostingdomain.Connector
	Verboe                  bool
	Lineage                 configdomain.Lineage
	InitialBranchesSnapshot gitdomain.BranchesStatus
	InitialConfigSnapshot   undoconfig.ConfigSnapshot
	InitialStashSnapshot    gitdomain.StashSize
	PushHook                configdomain.PushHook
	RootDir                 gitdomain.RepoRootDir
	Run                     *git.ProdRunner
}

func abortRunstate(runState *runstate.RunState, args UnfinishedStateArgs) (bool, error) {
	abortRunState := runState.CreateAbortRunState()
	return true, interpreter.Execute(interpreter.ExecuteArgs{
		FullConfig:              &args.Run.FullConfig,
		Connector:               args.Connector,
		Verbose:                 args.Verboe,
		InitialBranchesSnapshot: args.InitialBranchesSnapshot,
		InitialConfigSnapshot:   args.InitialConfigSnapshot,
		InitialStashSnapshot:    args.InitialStashSnapshot,
		RootDir:                 args.RootDir,
		Run:                     args.Run,
		RunState:                &abortRunState,
	})
}

func continueRunstate(runState *runstate.RunState, args UnfinishedStateArgs) (bool, error) {
	repoStatus, err := args.Run.Backend.RepoStatus()
	if err != nil {
		return false, err
	}
	if repoStatus.Conflicts {
		return false, fmt.Errorf(messages.ContinueUnresolvedConflicts)
	}
	return true, interpreter.Execute(interpreter.ExecuteArgs{
		FullConfig:              &args.Run.FullConfig,
		Connector:               args.Connector,
		Verbose:                 args.Verboe,
		InitialBranchesSnapshot: args.InitialBranchesSnapshot,
		InitialConfigSnapshot:   args.InitialConfigSnapshot,
		InitialStashSnapshot:    args.InitialStashSnapshot,
		RootDir:                 args.RootDir,
		Run:                     args.Run,
		RunState:                runState,
	})
}

func discardRunstate(rootDir gitdomain.RepoRootDir) (bool, error) {
	err := statefile.Delete(rootDir)
	return false, err
}

func skipRunstate(runState *runstate.RunState, args UnfinishedStateArgs) (bool, error) {
	skipRunState := runState.CreateSkipRunState()
	return true, interpreter.Execute(interpreter.ExecuteArgs{
		FullConfig:              &args.Run.FullConfig,
		Connector:               args.Connector,
		Verbose:                 args.Verboe,
		InitialBranchesSnapshot: args.InitialBranchesSnapshot,
		InitialConfigSnapshot:   args.InitialConfigSnapshot,
		InitialStashSnapshot:    args.InitialStashSnapshot,
		RootDir:                 args.RootDir,
		Run:                     args.Run,
		RunState:                &skipRunState,
	})
}
