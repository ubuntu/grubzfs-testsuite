package main_test

import (
	"sync"
	"testing"
	"time"
)

var testWaiter = struct {
	mu sync.RWMutex
	c  map[string]chan struct{}
}{
	c: make(map[string]chan struct{}),
}

// getChan create the channel for the given testName
func getChan(testName string) chan struct{} {
	testWaiter.mu.Lock()
	defer testWaiter.mu.Unlock()
	if testWaiter.c[testName] == nil {
		testWaiter.c[testName] = make(chan struct{}, 1)
	}
	return testWaiter.c[testName]
}

// registerTest registers current test to start, return the teardown to unregister the tests
func registerTest(t *testing.T) func() {
	tw := getChan(t.Name())
	tw <- struct{}{}
	return func() {
		close(tw)
	}
}

// waitForTest blocks until testName has fully ran. Timeout if the test didn't start
func waitForTest(t *testing.T, testName string) {
	tw := getChan(testName)
	select {
	case <-tw:
		// Testsuite has ther other test running
		// Wait now for the channel to close, indicating other test is done
		<-tw
	// We waited for long enough for the other tests to register. It will probably stay nil (filtered with -run)
	// and we can thus start our tests.
	case <-time.After(10 * time.Millisecond):
		t.Logf("Timeout reached when waiting for %q", testName)
	}
}
