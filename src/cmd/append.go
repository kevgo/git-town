package cmd

import (
	"slices"

	"github.com/git-town/git-town/v11/src/cli/flags"
	"github.com/git-town/git-town/v11/src/cmd/cmdhelpers"
	"github.com/git-town/git-town/v11/src/config/configdomain"
	"github.com/git-town/git-town/v11/src/execute"
	"github.com/git-town/git-town/v11/src/git/gitdomain"
	"github.com/git-town/git-town/v11/src/messages"
	"github.com/git-town/git-town/v11/src/sync"
	"github.com/git-town/git-town/v11/src/vm/interpreter"
	"github.com/git-town/git-town/v11/src/vm/opcode"
	"github.com/git-town/git-town/v11/src/vm/program"
	"github.com/git-town/git-town/v11/src/vm/runstate"
	"github.com/spf13/cobra"
)

const appendDesc = "Creates a new feature branch as a child of the current branch"

const appendHelp = `
Syncs the current branch,
forks a new feature branch with the given name off the current branch,
makes the new branch a child of the current branch,
pushes the new feature branch to the origin repository
(if and only if "push-new-branches" is true),
and brings over all uncommitted changes to the new feature branch.

See "sync" for information regarding upstream remotes.`

func appendCmd() *cobra.Command {
	addVerboseFlag, readVerboseFlag := flags.Verbose()
	addDryRunFlag, readDryRunFlag := flags.DryRun()
	cmd := cobra.Command{
		Use:     "append <branch>",
		GroupID: "lineage",
		Args:    cobra.ExactArgs(1),
		Short:   appendDesc,
		Long:    cmdhelpers.Long(appendDesc, appendHelp),
		RunE: func(cmd *cobra.Command, args []string) error {
			return executeAppend(args[0], readDryRunFlag(cmd), readVerboseFlag(cmd))
		},
	}
	addDryRunFlag(&cmd)
	addVerboseFlag(&cmd)
	return &cmd
}

func executeAppend(arg string, dryRun, verbose bool) error {
	repo, err := execute.OpenRepo(execute.OpenRepoArgs{
		Verbose:          verbose,
		DryRun:           dryRun,
		OmitBranchNames:  false,
		PrintCommands:    true,
		ValidateIsOnline: false,
		ValidateGitRepo:  true,
	})
	if err != nil {
		return err
	}
	config, initialBranchesSnapshot, initialStashSnapshot, exit, err := determineAppendConfig(gitdomain.NewLocalBranchName(arg), repo, dryRun, verbose)
	if err != nil || exit {
		return err
	}
	runState := runstate.RunState{
		Command:             "append",
		DryRun:              dryRun,
		InitialActiveBranch: initialBranchesSnapshot.Active,
		RunProgram:          appendProgram(config),
	}
	return interpreter.Execute(interpreter.ExecuteArgs{
		FullConfig:              config.FullConfig,
		RunState:                &runState,
		Run:                     repo.Runner,
		Connector:               nil,
		Verbose:                 verbose,
		RootDir:                 repo.RootDir,
		InitialBranchesSnapshot: initialBranchesSnapshot,
		InitialConfigSnapshot:   repo.ConfigSnapshot,
		InitialStashSnapshot:    initialStashSnapshot,
	})
}

type appendConfig struct {
	*configdomain.FullConfig
	branches                  configdomain.Branches
	branchesToSync            gitdomain.BranchInfos
	dryRun                    bool
	hasOpenChanges            bool
	remotes                   gitdomain.Remotes
	newBranchParentCandidates gitdomain.LocalBranchNames
	parentBranch              gitdomain.LocalBranchName
	previousBranch            gitdomain.LocalBranchName
	targetBranch              gitdomain.LocalBranchName
}

func determineAppendConfig(targetBranch gitdomain.LocalBranchName, repo *execute.OpenRepoResult, dryRun, verbose bool) (*appendConfig, gitdomain.BranchesStatus, gitdomain.StashSize, bool, error) {
	fc := execute.FailureCollector{}
	branches, branchesSnapshot, stashSnapshot, exit, err := execute.LoadBranches(execute.LoadBranchesArgs{
		FullConfig:            &repo.Runner.FullConfig,
		Repo:                  repo,
		Verbose:               verbose,
		Fetch:                 true,
		HandleUnfinishedState: true,
		ValidateIsConfigured:  true,
		ValidateNoOpenChanges: false,
	})
	if err != nil || exit {
		return nil, branchesSnapshot, stashSnapshot, exit, err
	}
	previousBranch := repo.Runner.Backend.PreviouslyCheckedOutBranch()
	remotes := fc.Remotes(repo.Runner.Backend.Remotes())
	repoStatus := fc.RepoStatus(repo.Runner.Backend.RepoStatus())
	if fc.Err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, fc.Err
	}
	if branches.All.HasLocalBranch(targetBranch) {
		fc.Fail(messages.BranchAlreadyExistsLocally, targetBranch)
	}
	if branches.All.HasMatchingTrackingBranchFor(targetBranch) {
		fc.Fail(messages.BranchAlreadyExistsRemotely, targetBranch)
	}
	branches.Types, repo.Runner.Lineage, err = execute.EnsureKnownBranchAncestry(branches.Initial, execute.EnsureKnownBranchAncestryArgs{
		FullConfig:    &repo.Runner.FullConfig,
		AllBranches:   branches.All,
		BranchTypes:   branches.Types,
		DefaultBranch: repo.Runner.MainBranch,
		Runner:        repo.Runner,
	})
	if err != nil {
		return nil, branchesSnapshot, stashSnapshot, false, err
	}
	branchNamesToSync := repo.Runner.Lineage.BranchAndAncestors(branches.Initial)
	branchesToSync := fc.BranchesSyncStatus(branches.All.Select(branchNamesToSync))
	initialAndAncestors := repo.Runner.Lineage.BranchAndAncestors(branches.Initial)
	slices.Reverse(initialAndAncestors)
	return &appendConfig{
		branches:                  branches,
		branchesToSync:            branchesToSync,
		FullConfig:                &repo.Runner.FullConfig,
		dryRun:                    dryRun,
		hasOpenChanges:            repoStatus.OpenChanges,
		remotes:                   remotes,
		newBranchParentCandidates: initialAndAncestors,
		parentBranch:              branches.Initial,
		previousBranch:            previousBranch,
		targetBranch:              targetBranch,
	}, branchesSnapshot, stashSnapshot, false, fc.Err
}

func appendProgram(config *appendConfig) program.Program {
	prog := program.Program{}
	for _, branch := range config.branchesToSync {
		sync.BranchProgram(branch, sync.BranchProgramArgs{
			FullConfig:  config.FullConfig,
			BranchInfos: config.branches.All,
			BranchTypes: config.branches.Types,
			Program:     &prog,
			Remotes:     config.remotes,
			PushBranch:  true,
		})
	}
	prog.Add(&opcode.CreateBranchExistingParent{
		Ancestors: config.newBranchParentCandidates,
		Branch:    config.targetBranch,
	})
	prog.Add(&opcode.SetExistingParent{
		Branch:    config.targetBranch,
		Ancestors: config.newBranchParentCandidates,
	})
	prog.Add(&opcode.Checkout{Branch: config.targetBranch})
	if config.remotes.HasOrigin() && config.ShouldNewBranchPush() && config.IsOnline() {
		prog.Add(&opcode.CreateTrackingBranch{Branch: config.targetBranch})
	}
	cmdhelpers.Wrap(&prog, cmdhelpers.WrapOptions{
		DryRun:                   config.dryRun,
		RunInGitRoot:             true,
		StashOpenChanges:         config.hasOpenChanges,
		PreviousBranchCandidates: gitdomain.LocalBranchNames{config.branches.Initial, config.previousBranch},
	})
	return prog
}
