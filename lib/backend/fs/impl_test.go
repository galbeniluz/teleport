package fs

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gravitational/teleport/lib/backend"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/trace"

	"gopkg.in/check.v1"
)

type FrozenTime struct {
	sync.Mutex
	CurrentTime time.Time
}

func (t *FrozenTime) UtcNow() time.Time {
	t.Lock()
	defer t.Unlock()
	return t.CurrentTime
}

func (t *FrozenTime) Sleep(d time.Duration) {
	t.Lock()
	defer t.Unlock()
	t.CurrentTime = t.CurrentTime.Add(d)
}

func (t *FrozenTime) After(d time.Duration) <-chan time.Time {
	t.Sleep(d)
	c := make(chan time.Time, 1)
	c <- t.CurrentTime
	return c
}

type Suite struct {
	dirName string
	bk      backend.Backend
	clock   FrozenTime
}

var _ = check.Suite(&Suite{})

// bootstrap check.v1:
func TestFSBackend(t *testing.T) { check.TestingT(t) }

func (s *Suite) SetUpSuite(c *check.C) {
	dirName := c.MkDir()
	bk, err := FromJSON(fmt.Sprintf(`{ "path": "%s" }`, dirName))

	c.Assert(err, check.IsNil)
	c.Assert(bk.RootDir, check.Equals, dirName)
	c.Assert(utils.IsDir(bk.RootDir), check.Equals, true)

	bk.Clock = &s.clock
	s.bk = bk
}

func (s *Suite) TestCreateAndRead(c *check.C) {
	bucket := []string{"one", "two"}

	// must succeed:
	err := s.bk.CreateVal(bucket, "key", []byte("original"), backend.Forever)
	c.Assert(err, check.IsNil)

	// must get 'already exists' error
	err = s.bk.CreateVal(bucket, "key", []byte("failed-write"), backend.Forever)
	c.Assert(trace.IsAlreadyExists(err), check.Equals, true)

	// read back the original:
	val, err := s.bk.GetVal(bucket, "key")
	c.Assert(err, check.IsNil)
	c.Assert(string(val), check.Equals, "original")

	// upsert:
	err = s.bk.UpsertVal(bucket, "key", []byte("new-value"), backend.Forever)
	c.Assert(err, check.IsNil)

	// read back the new value:
	val, err = s.bk.GetVal(bucket, "key")
	c.Assert(err, check.IsNil)
	c.Assert(string(val), check.Equals, "new-value")

	// read back non-existing (bad path):
	val, err = s.bk.GetVal([]string{"bad", "path"}, "key")
	c.Assert(err, check.NotNil)
	c.Assert(val, check.IsNil)
	c.Assert(trace.IsNotFound(err), check.Equals, true)

	// read back non-existing (bad key):
	val, err = s.bk.GetVal(bucket, "bad-key")
	c.Assert(err, check.NotNil)
	c.Assert(val, check.IsNil)
	c.Assert(trace.IsNotFound(err), check.Equals, true)
}

func (s *Suite) TestListDelete(c *check.C) {
	root := []string{"root"}
	kid := []string{"root", "kid"}

	// list from non-existing bucket (must return an empty array)
	kids, err := s.bk.GetKeys([]string{"bad", "bucket"})
	c.Assert(err, check.IsNil)
	c.Assert(kids, check.HasLen, 0)

	// create two entries in root:
	s.bk.CreateVal(root, "one", []byte("1"), backend.Forever)
	s.bk.CreateVal(root, "two", []byte("2"), time.Second)

	// create one entry in the kid:
	s.bk.CreateVal(kid, "three", []byte("3"), backend.Forever)

	// list the root (should get 2 back):
	kids, err = s.bk.GetKeys(root)
	c.Assert(err, check.IsNil)
	c.Assert(kids, check.HasLen, 2)
	c.Assert(kids[0], check.Equals, "one")
	c.Assert(kids[1], check.Equals, "two")

	// list the kid (should get 1)
	kids, err = s.bk.GetKeys(kid)
	c.Assert(err, check.IsNil)
	c.Assert(kids, check.HasLen, 1)
	c.Assert(kids[0], check.Equals, "three")

	// delete one of the kids:
	err = s.bk.DeleteKey(kid, "three")
	c.Assert(err, check.IsNil)
	kids, err = s.bk.GetKeys(kid)
	c.Assert(kids, check.HasLen, 0)

	// try to delete non-existing key:
	err = s.bk.DeleteKey(kid, "three")
	c.Assert(trace.IsNotFound(err), check.Equals, true)

	// try to delete the root bucket:
	err = s.bk.DeleteBucket(root, "kid")
	c.Assert(err, check.IsNil)
}

func (s *Suite) TestTTL(c *check.C) {
	bucket := []string{"root"}
	value := []byte("value")

	s.bk.CreateVal(bucket, "key", value, time.Second)
	v, err := s.bk.GetVal(bucket, "key")
	c.Assert(err, check.IsNil)
	c.Assert(string(v), check.Equals, string(value))

	// after sleeping for 2 seconds the value must be gone:
	s.clock.Sleep(time.Second * 2)
	v, err = s.bk.GetVal(bucket, "key")
	c.Assert(trace.IsNotFound(err), check.Equals, true)
	c.Assert(err.Error(), check.Equals, `key 'key' is not found`)
	c.Assert(v, check.IsNil)
}

func (s *Suite) TestLock(c *check.C) {
	var protectedFlag int64 = 1
	defer s.bk.ReleaseLock("lock")

	err := s.bk.AcquireLock("lock", time.Second)
	c.Assert(err, check.IsNil)

	go func() {
		defer s.bk.ReleaseLock("lock")
		s.bk.AcquireLock("lock", time.Second)
		atomic.AddInt64(&protectedFlag, 1)
	}()

	s.clock.Sleep(time.Millisecond)
	c.Assert(atomic.LoadInt64(&protectedFlag), check.Equals, int64(1))
}

func (s *Suite) TestLockTTL(c *check.C) {
	var protectedFlag int64 = 1
	ln := "ttl-test"

	err := s.bk.AcquireLock(ln, time.Second)
	c.Assert(err, check.IsNil)
	defer s.bk.ReleaseLock(ln)

	go func() {
		s.bk.AcquireLock(ln, time.Minute)
		defer s.bk.ReleaseLock(ln)
		atomic.AddInt64(&protectedFlag, 1)
	}()

	time.Sleep(time.Millisecond * 3) // give the goroutine some time to start

	// wait for 5 seconds. this should be enough for the 1st lock
	// to expire and the goroutine should be able to flip the flag
	s.clock.Sleep(time.Second * 5)
	c.Assert(atomic.LoadInt64(&protectedFlag), check.Equals, int64(2))
}
