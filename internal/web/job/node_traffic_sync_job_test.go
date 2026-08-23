package job

import (
	"errors"
	"sync"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func TestAccountingOnlyNodeCandidateIncludesDisabledNodes(t *testing.T) {
	if shouldPollSingboxAccounting(&model.Node{Enable: true}) {
		t.Fatal("enabled node must use normal traffic sync")
	}
	if !shouldPollSingboxAccounting(&model.Node{Enable: false}) {
		t.Fatal("disabled node should be eligible for accounting-only polling")
	}
}

func TestAccountingOnlyQuotaRunsWhenSnapshotFails(t *testing.T) {
	var quotaCalls int
	snapshotErr := errors.New("counter unavailable")
	quotaErr := errors.New("mutation failed")
	applied, gotSnapshotErr, gotQuotaErr := runAccountingOnlyPaths(
		func() (bool, error) { return false, snapshotErr },
		func() error { quotaCalls++; return quotaErr },
	)
	if applied || !errors.Is(gotSnapshotErr, snapshotErr) || !errors.Is(gotQuotaErr, quotaErr) || quotaCalls != 1 {
		t.Fatalf("applied=%v snapshot=%v quota=%v calls=%d", applied, gotSnapshotErr, gotQuotaErr, quotaCalls)
	}
}

func TestAccountingOnlyQuotaRunsWhenSnapshotSucceeds(t *testing.T) {
	var quotaCalls int
	applied, snapshotErr, quotaErr := runAccountingOnlyPaths(
		func() (bool, error) { return true, nil },
		func() error { quotaCalls++; return nil },
	)
	if !applied || snapshotErr != nil || quotaErr != nil || quotaCalls != 1 {
		t.Fatalf("applied=%v snapshot=%v quota=%v calls=%d", applied, snapshotErr, quotaErr, quotaCalls)
	}
}

func TestLocalDepletionRunsOnlyWithNormalNodePolls(t *testing.T) {
	if shouldRunLocalDepletion(0) {
		t.Fatal("accounting-only master must not run local depletion")
	}
	if !shouldRunLocalDepletion(1) {
		t.Fatal("master with a normal online node must run local depletion")
	}
}

func TestAtomicBool_DefaultIsFalse(t *testing.T) {
	var a atomicBool
	if a.takeAndReset() {
		t.Fatal("default atomicBool should report false")
	}
}

func TestAtomicBool_SetThenTakeReturnsTrueOnce(t *testing.T) {
	var a atomicBool
	a.set()
	if !a.takeAndReset() {
		t.Fatal("takeAndReset after set should return true")
	}
	if a.takeAndReset() {
		t.Fatal("second takeAndReset should return false (state was reset)")
	}
}

func TestAtomicBool_SetIsIdempotent(t *testing.T) {
	var a atomicBool
	a.set()
	a.set()
	a.set()
	if !a.takeAndReset() {
		t.Fatal("repeated set should still leave the flag true")
	}
	if a.takeAndReset() {
		t.Fatal("flag should be cleared after the first take")
	}
}

func TestAtomicBool_ConcurrentSettersExactlyOneTakeWins(t *testing.T) {
	var a atomicBool
	const setters = 100
	const readers = 20

	var wg sync.WaitGroup
	for range setters {
		wg.Go(func() {
			a.set()
		})
	}
	wg.Wait()

	trueCount := 0
	var rwg sync.WaitGroup
	var mu sync.Mutex
	for range readers {
		rwg.Go(func() {
			if a.takeAndReset() {
				mu.Lock()
				trueCount++
				mu.Unlock()
			}
		})
	}
	rwg.Wait()

	if trueCount != 1 {
		t.Fatalf("expected exactly one reader to observe true, got %d", trueCount)
	}
}
