package storefs_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	commandmodel "github.com/digikeeper/digikeeper-journal/internal/domain/command/model"
	"github.com/digikeeper/digikeeper-journal/internal/domain/core"
	"github.com/digikeeper/digikeeper-journal/internal/infrastructure/storefs"
)

// TestCandidateDirs_CoverEveryDomainState ties the domain's set
// of states and the directories storage keeps them in. Adding a state to
// core.CandidateStates without giving it a directory fails here, rather than
// silently writing those candidates nowhere.
func TestCandidateDirs_CoverEveryDomainState(t *testing.T) {
	dir := open(t)
	partition := core.PartitionFromTime(someTime(t))

	seen := map[string]core.CandidateState{}
	for _, state := range core.CandidateStates() {
		candidateDir, ok := storefs.CandidateDir(state)
		require.Truef(t, ok, "state %q has no directory", state)
		require.NotEmptyf(t, candidateDir, "state %q maps to an empty directory", state)

		if other, clash := seen[candidateDir]; clash {
			t.Fatalf("states %q and %q share directory %q", other, state, candidateDir)
		}
		seen[candidateDir] = state

		path, err := dir.CandidatePath(state, partition)
		require.NoError(t, err)
		require.DirExistsf(t, filepath.Dir(filepath.Dir(path)),
			"Open must create the directory for state %q", state)
	}
}

// TestResolutionStates_HaveSomewhereToRest checks the other half: every way a
// candidate can be resolved leads to a state, and that state has a directory.
func TestResolutionStates_HaveSomewhereToRest(t *testing.T) {
	for _, action := range []commandmodel.CandidateResolution{commandmodel.ApplyResolution, commandmodel.DenyResolution} {
		state, ok := action.EndState()
		require.Truef(t, ok, "resolution %q resolves to no state", action)

		_, ok = storefs.CandidateDir(state)
		require.Truef(t, ok, "resolution %q rests in state %q, which has no directory", action, state)
	}

	_, ok := commandmodel.CandidateResolution("defer").EndState()
	require.False(t, ok, "an unknown resolution must not resolve to a state")
}

// TestCandidatePath_RejectsUnknownState proves the failure is reported rather
// than silently producing a path outside the known areas.
func TestCandidatePath_RejectsUnknownState(t *testing.T) {
	_, err := open(t).CandidatePath(core.CandidateState("archived"), core.PartitionFromTime(someTime(t)))
	require.Error(t, err)
}
