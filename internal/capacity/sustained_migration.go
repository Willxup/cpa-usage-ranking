package capacity

// This is a single explicitly reviewed C54 journal transition, not automatic
// recovery of failed budgets or a general permission to change replay options.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const c54RetryApproval = "c54-721cb71e50f60575-retry-30-to-512"
const c54RetryBudgetSHA = "dc40ff7f5adb81424a5dacd948ecddb9edfac6528106b04f27f4643a2a9768ae"
const c54RetryOldConfigHash = "c9fe089a5bfe2ab3fa0079dfe30d07aeeb247a1f55eaea6f9203ec19bfe77bf8"
const c54RetryOldBinarySHA = "fbebd0bca029514e0a3728e238fe0986301ea7a23ecd3d704dbb8443e062546a"

// A variable solely to let the isolated CLI integration test build its own
// pinned synthetic prefix. The normal binary pins the parent-audited journal.
const c54RealRetryPrefixSHA = "d08cb32d0c2b0dfc7bf0589d80ba6142a6d9be44c8a84c54bab922830a851c6e"

var c54RetryPrefixSHA = c54RealRetryPrefixSHA

type sustainedRetryMigration struct {
	Approval           string `json:"approval"`
	PreviousJournalSHA string `json:"previous_journal_sha256"`
	PreviousBytes      int64  `json:"previous_journal_bytes"`
	PreviousBudgetSHA  string `json:"previous_budget_sha256"`
	PreviousConfigHash string `json:"previous_config_hash"`
	NextConfigHash     string `json:"next_config_hash"`
	PreviousBinarySHA  string `json:"previous_binary_sha256"`
	NextBinarySHA      string `json:"next_binary_sha256"`
	PreviousAttempts   int    `json:"previous_max_attempts"`
	NextAttempts       int    `json:"next_max_attempts"`
	PreviousPending    int    `json:"previous_max_pending"`
	NextPending        int    `json:"next_max_pending"`
	TaskSequence       int64  `json:"task_sequence"`
	TaskClient         int    `json:"task_client"`
	TaskRevision       int64  `json:"task_revision"`
	TaskDay            string `json:"task_day"`
	TaskBodySHA        string `json:"task_body_sha256"`
	TaskAttempts       int    `json:"task_attempts"`
	NextTaskAttempt    int    `json:"next_task_attempt"`
}

func validSHA(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

func (m *sustainedRetryMigration) validate(file *os.File) error {
	if m == nil || m.Approval != c54RetryApproval || m.PreviousJournalSHA != c54RetryPrefixSHA ||
		m.PreviousBudgetSHA != c54RetryBudgetSHA || m.PreviousConfigHash != c54RetryOldConfigHash ||
		m.PreviousBinarySHA != c54RetryOldBinarySHA || !validSHA(m.NextConfigHash) || !validSHA(m.NextBinarySHA) ||
		m.PreviousAttempts != 30 || m.NextAttempts != 512 || m.PreviousPending != 125000 ||
		(m.NextPending != 125000 && m.NextPending != 100000) || m.PreviousBytes <= 0 ||
		m.TaskSequence <= 0 || m.TaskClient <= 0 || m.TaskRevision <= 0 || m.TaskDay == "" || !validSHA(m.TaskBodySHA) ||
		m.TaskAttempts != 30 || m.NextTaskAttempt != 31 {
		return errors.New("unapproved C54 retry migration")
	}
	if c54RetryPrefixSHA == c54RealRetryPrefixSHA &&
		(m.TaskSequence != 130422 || m.TaskClient != 9034 || m.TaskRevision != 54 || m.TaskDay != "2026-11-13" ||
			m.TaskBodySHA != "ad6e280211e23aa21566bdad0074a0045452d71dbb1ced4d748aea1b21448a1a") {
		return errors.New("reviewed retry task identity differs from audited task")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.NewSectionReader(file, 0, m.PreviousBytes))
	if err != nil || n != m.PreviousBytes || hex.EncodeToString(h.Sum(nil)) != m.PreviousJournalSHA {
		return errors.New("reviewed journal prefix does not match")
	}
	return nil
}

func verifyReviewedRetryTask(m *sustainedRetryMigration, state *sustainedState) error {
	a := state.actors[m.TaskClient]
	if a == nil || !a.registered || a.inFlight || len(a.queue) == 0 {
		return errors.New("reviewed retry task is not an idle registered queue head")
	}
	job := a.queue[0]
	if job.task.Sequence != m.TaskSequence || job.task.Revision != m.TaskRevision || job.task.Day != m.TaskDay ||
		job.task.BodyHash != m.TaskBodySHA || job.attempts != m.TaskAttempts {
		return errors.New("reviewed retry task identity, body or cumulative attempts changed")
	}
	return nil
}

func loadSustainedRetryMigration(path string, file *os.File) (*sustainedRetryMigration, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 8193))
	dec.DisallowUnknownFields()
	var m sustainedRetryMigration
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("invalid retry migration manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing retry migration manifest data")
	}
	if err := m.validate(file); err != nil {
		return nil, err
	}
	return &m, nil
}
