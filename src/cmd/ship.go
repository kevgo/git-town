package cmd

import (
	"fmt"
	"strings"

	"github.com/git-town/git-town/v7/src/cli"
	"github.com/git-town/git-town/v7/src/config"
	"github.com/git-town/git-town/v7/src/dialog"
	"github.com/git-town/git-town/v7/src/git"
	"github.com/git-town/git-town/v7/src/hosting"
	"github.com/git-town/git-town/v7/src/runstate"
	"github.com/git-town/git-town/v7/src/steps"
	"github.com/spf13/cobra"
)

type shipConfig struct {
	branchToShip            string
	branchToMergeInto       string
	canShipWithDriver       bool
	childBranches           []string
	defaultCommitMessage    string
	hasOrigin               bool
	hasTrackingBranch       bool
	initialBranch           string
	isShippingInitialBranch bool
	isOffline               bool
	pullRequestNumber       int64
	deleteOriginBranch      bool
}

func shipCmd(repo *git.ProdRepo) *cobra.Command {
	var commitMessage string
	shipCmd := cobra.Command{
		Use:   "ship",
		Short: "Deliver a completed feature branch",
		Long: fmt.Sprintf(`Deliver a completed feature branch

Squash-merges the current branch, or <branch_name> if given,
into the main branch, resulting in linear history on the main branch.

- syncs the main branch
- pulls updates for <branch_name>
- merges the main branch into <branch_name>
- squash-merges <branch_name> into the main branch
  with commit message specified by the user
- pushes the main branch to the origin repository
- deletes <branch_name> from the local and origin repositories

Ships direct children of the main branch.
To ship a nested child branch, ship or kill all ancestor branches first.

If you use GitHub, this command can squash merge pull requests via the GitHub API. Setup:
1. Get a GitHub personal access token with the "repo" scope
2. Run 'git config %s <token>' (optionally add the '--global' flag)
Now anytime you ship a branch with a pull request on GitHub, it will squash merge via the GitHub API.
It will also update the base branch for any pull requests against that branch.

If your origin server deletes shipped branches, for example
GitHub's feature to automatically delete head branches,
run "git config %s false"
and Git Town will leave it up to your origin server to delete the remote branch.`, config.GithubToken, config.ShipDeleteRemoteBranch),
		Run: func(cmd *cobra.Command, args []string) {
			driver, err := hosting.NewDriver(&repo.Config, &repo.Silent, cli.PrintDriverAction)
			if err != nil {
				cli.Exit(err)
			}
			config, err := determineShipConfig(args, driver, repo)
			if err != nil {
				cli.Exit(err)
			}
			stepList, err := shipStepList(config, commitMessage, repo)
			if err != nil {
				cli.Exit(err)
			}
			runState := runstate.New("ship", stepList)
			err = runstate.Execute(runState, repo, driver)
			if err != nil {
				cli.Exit(err)
			}
		},
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := ValidateIsRepository(repo); err != nil {
				return err
			}
			return validateIsConfigured(repo)
		},
	}
	shipCmd.Flags().StringVarP(&commitMessage, "message", "m", "", "Specify the commit message for the squash commit")
	return &shipCmd
}

func determineShipConfig(args []string, driver hosting.Driver, repo *git.ProdRepo) (*shipConfig, error) {
	initialBranch, err := repo.Silent.CurrentBranch()
	if err != nil {
		return nil, err
	}
	var branchToShip string
	if len(args) == 0 {
		branchToShip = initialBranch
	} else {
		branchToShip = args[0]
	}
	isShippingInitialBranch := branchToShip == initialBranch
	if isShippingInitialBranch {
		hasOpenChanges, err := repo.Silent.HasOpenChanges()
		if err != nil {
			return nil, err
		}
		if hasOpenChanges {
			return nil, fmt.Errorf("you have uncommitted changes. Did you mean to commit them before shipping?")
		}
	}
	hasOrigin, err := repo.Silent.HasOrigin()
	if err != nil {
		return nil, err
	}
	isOffline, err := repo.Config.IsOffline()
	if err != nil {
		return nil, err
	}
	if hasOrigin && !isOffline {
		err := repo.Logging.Fetch()
		if err != nil {
			return nil, err
		}
	}
	if !isShippingInitialBranch {
		hasBranch, err := repo.Silent.HasLocalOrOriginBranch(branchToShip)
		if err != nil {
			return nil, err
		}
		if !hasBranch {
			return nil, fmt.Errorf("there is no branch named %q", branchToShip)
		}
	}
	if !repo.Config.IsFeatureBranch(branchToShip) {
		return nil, fmt.Errorf("the branch %q is not a feature branch. Only feature branches can be shipped", branchToShip)
	}
	parentDialog := dialog.ParentBranches{}
	err = parentDialog.EnsureKnowsParentBranches([]string{branchToShip}, repo)
	if err != nil {
		return nil, err
	}
	ensureParentBranchIsMainOrPerennialBranch(branchToShip, repo)
	hasTrackingBranch, err := repo.Silent.HasTrackingBranch(branchToShip)
	if err != nil {
		return nil, err
	}
	branchToMergeInto := repo.Config.ParentBranch(branchToShip)
	prInfo, err := determinePullRequestInfo(branchToShip, branchToMergeInto, repo, driver)
	if err != nil {
		return nil, err
	}
	deleteOrigin, err := repo.Config.ShouldShipDeleteOriginBranch()
	if err != nil {
		return nil, err
	}
	return &shipConfig{
		isOffline:               isOffline,
		isShippingInitialBranch: isShippingInitialBranch,
		branchToMergeInto:       branchToMergeInto,
		branchToShip:            branchToShip,
		canShipWithDriver:       prInfo.CanMergeWithAPI,
		childBranches:           repo.Config.ChildBranches(branchToShip),
		defaultCommitMessage:    prInfo.DefaultCommitMessage,
		deleteOriginBranch:      deleteOrigin,
		hasOrigin:               hasOrigin,
		hasTrackingBranch:       hasTrackingBranch,
		initialBranch:           initialBranch,
		pullRequestNumber:       prInfo.PullRequestNumber,
	}, nil
}

func ensureParentBranchIsMainOrPerennialBranch(branch string, repo *git.ProdRepo) {
	parentBranch := repo.Config.ParentBranch(branch)
	if !repo.Config.IsMainBranch(parentBranch) && !repo.Config.IsPerennialBranch(parentBranch) {
		ancestors := repo.Config.AncestorBranches(branch)
		ancestorsWithoutMainOrPerennial := ancestors[1:]
		oldestAncestor := ancestorsWithoutMainOrPerennial[0]
		cli.Exit(fmt.Errorf(`shipping this branch would ship %q as well,
please ship %q first`, strings.Join(ancestorsWithoutMainOrPerennial, ", "), oldestAncestor))
	}
}

func shipStepList(config *shipConfig, commitMessage string, repo *git.ProdRepo) (runstate.StepList, error) {
	syncSteps, err := syncBranchSteps(config.branchToMergeInto, true, repo)
	if err != nil {
		return runstate.StepList{}, err
	}
	result := runstate.StepList{}
	result.AppendList(syncSteps)
	syncSteps, err = syncBranchSteps(config.branchToShip, false, repo)
	if err != nil {
		return runstate.StepList{}, err
	}
	result.AppendList(syncSteps)
	result.Append(&steps.EnsureHasShippableChangesStep{Branch: config.branchToShip})
	result.Append(&steps.CheckoutBranchStep{Branch: config.branchToMergeInto})
	if config.canShipWithDriver {
		result.Append(&steps.PushBranchStep{Branch: config.branchToShip})
		result.Append(&steps.DriverMergePullRequestStep{
			Branch:               config.branchToShip,
			PullRequestNumber:    config.pullRequestNumber,
			CommitMessage:        commitMessage,
			DefaultCommitMessage: config.defaultCommitMessage,
		})
		result.Append(&steps.PullBranchStep{})
	} else {
		result.Append(&steps.SquashMergeBranchStep{Branch: config.branchToShip, CommitMessage: commitMessage})
	}
	if config.hasOrigin && !config.isOffline {
		result.Append(&steps.PushBranchStep{Branch: config.branchToMergeInto, Undoable: true})
	}
	// NOTE: when shipping with a driver, we can always delete the remote branch because:
	// - we know we have a tracking branch (otherwise there would be no PR to ship via driver)
	// - we have updated the PRs of all child branches (because we have API access)
	// - we know we are online
	if config.canShipWithDriver || (config.hasTrackingBranch && len(config.childBranches) == 0 && !config.isOffline) {
		if config.deleteOriginBranch {
			result.Append(&steps.DeleteOriginBranchStep{Branch: config.branchToShip, IsTracking: true})
		}
	}
	result.Append(&steps.DeleteLocalBranchStep{Branch: config.branchToShip})
	result.Append(&steps.DeleteParentBranchStep{Branch: config.branchToShip})
	for _, child := range config.childBranches {
		result.Append(&steps.SetParentBranchStep{Branch: child, ParentBranch: config.branchToMergeInto})
	}
	if !config.isShippingInitialBranch {
		result.Append(&steps.CheckoutBranchStep{Branch: config.initialBranch})
	}
	err = result.Wrap(runstate.WrapOptions{RunInGitRoot: true, StashOpenChanges: !config.isShippingInitialBranch}, repo)
	return result, err
}

func determinePullRequestInfo(branch, parentBranch string, repo *git.ProdRepo, driver hosting.Driver) (hosting.PullRequestInfo, error) {
	hasOrigin, err := repo.Silent.HasOrigin()
	if err != nil {
		return hosting.PullRequestInfo{}, err
	}
	if !hasOrigin {
		return hosting.PullRequestInfo{}, nil
	}
	isOffline, err := repo.Config.IsOffline()
	if err != nil {
		return hosting.PullRequestInfo{}, err
	}
	if isOffline {
		return hosting.PullRequestInfo{}, nil
	}
	if driver == nil {
		return hosting.PullRequestInfo{}, nil
	}
	return driver.LoadPullRequestInfo(branch, parentBranch)
}
