package main_test

import (
	"testing"
	"time"
)

var testWaiter = make(map[string]chan struct{})

// registerTest registers current test to start, return the teardown to unregister the tests
func registerTest(t *testing.T) func() {
	testWaiter[t.Name()] = make(chan struct{}, 1)
	testWaiter[t.Name()] <- struct{}{}
	return func() {
		close(testWaiter[t.Name()])
	}
}

// waitForTest blocks until testName has fully ran. Timeout if the test didn't start
func waitForTest(t *testing.T, testName string) {
	select {
	case <-testWaiter[testName]:
		// Testsuite has ther other test running
		// Wait now for the channel to close, indicating other test is done
		<-testWaiter[testName]
	// We waited for long enough for the other tests to register. It will probably stay nil (filtered with -run)
	// and we can thus start our tests.
	case <-time.After(time.Second):
		t.Logf("timeout reached when waiting for %q", testName)
	}
}
