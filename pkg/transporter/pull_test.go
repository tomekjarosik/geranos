package transporter

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tomekjarosik/geranos/pkg/layout"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func calculateAccessed(rec []http.Request, method string, substr string) int {
	counter := 0
	for _, r := range rec {
		if r.Method == method && strings.Contains(r.URL.String(), substr) {
			counter += 1
		}
	}
	return counter
}

func TestPullAndPush_pullingAgainShouldNotDownloadAnyBlob(t *testing.T) {
	recordedRequests := make([]http.Request, 0)
	s := httptest.NewServer(prepareRegistryWithRecorder(&recordedRequests))
	defer s.Close()

	tempDir, opts := optionsForTesting(t)

	ref := refOnServer(s.URL, "test-vm:1.0")

	t.Run("pulling reference that does not exist", func(t *testing.T) {
		err := Pull(ref, opts...)
		assert.ErrorContains(t, err, "NAME_UNKNOWN: Unknown name")
	})

	shaBefore := makeTestVMAt(t, tempDir, ref)
	err := Push(ref, opts...)
	assert.NoError(t, err)

	deleteTestVMAt(t, tempDir, ref)

	t.Run("pulling reference for the first time", func(t *testing.T) {
		err = Pull(ref, opts...)
		assert.NoError(t, err)
		shaAfter := hashFromFile(t, filepath.Join(tempDir, "images", ref, "disk.img"))
		assert.Equal(t, shaBefore, shaAfter)
		assert.Equal(t, 2, calculateAccessed(recordedRequests, "GET", "/blobs"))
	})
	clear(recordedRequests)

	t.Run("pulling same reference second time", func(t *testing.T) {
		err = Pull(ref, opts...)
		assert.NoError(t, err)
		shaAfter := hashFromFile(t, filepath.Join(tempDir, "images", ref, "disk.img"))
		assert.Equal(t, shaBefore, shaAfter)

		assert.Equal(t, 0, calculateAccessed(recordedRequests, "GET", "/blobs"))
	})
}

func TestPullAndPush_multipleSlightlyDifferentTags(t *testing.T) {
	recordedRequests := make([]http.Request, 0)
	s := httptest.NewServer(prepareRegistryWithRecorder(&recordedRequests))
	defer s.Close()

	tempDir, opts := optionsForTesting(t)

	checksumsUploaded := make([]string, 5)
	t.Run("pushing must avoid uploading same blobs to remote", func(t *testing.T) {
		ref := refOnServer(s.URL, "test-vm:1.0")
		makeBigTestVMAt(t, tempDir, ref)

		expectedBlobUploads := []int{0, 4, 6, 8, 11}
		for i := 1; i <= 4; i++ {
			ithRef := refOnServer(s.URL, fmt.Sprintf("test-vm:1.%d", i))
			err := Clone(ref, ithRef, opts...)
			require.NoError(t, err)
			checksumsUploaded[i] = modifyBigTestVMAt(t, tempDir, ithRef, int64(1+i*17))
			if i == 4 {
				checksumsUploaded[i] = modifyBigTestVMAt(t, tempDir, ithRef, int64(64*1024*1024+i*18))
			}
			err = Push(ithRef, opts...)
			require.NoError(t, err)
			assert.Equal(t, expectedBlobUploads[i], calculateAccessed(recordedRequests, "PUT", "/blobs"))
		}
	})

	err := os.RemoveAll(tempDir)
	require.NoError(t, err)

	tempDir, opts = optionsForTesting(t)

	t.Run("pulling must avoid downloading same blobs", func(t *testing.T) {
		expectedBlobDownloads := []int{0, 3, 4, 5, 7}
		checksumsDownloaded := make([]string, 5)
		for i := 1; i <= 4; i++ {
			ithRef := refOnServer(s.URL, fmt.Sprintf("test-vm:1.%d", i))
			err = Pull(ithRef, opts...)
			require.NoError(t, err)
			assert.Equal(t, expectedBlobDownloads[i], calculateAccessed(recordedRequests, "GET", "/blobs"))
			checksumsDownloaded[i] = hashFromFile(t, filepath.Join(tempDir, "images", ithRef, "disk.img"))
			assert.Equal(t, checksumsUploaded[i], checksumsDownloaded[i])
		}
	})

	t.Run("pulling must preserve disk space", func(t *testing.T) {
		// TODO:
		fmt.Println(layout.DirectoryDiskUsage(tempDir))
	})
}

func TestPullAndPush_pullSmallerImageAfterPullingLargerImage(t *testing.T) {
	recordedRequests := make([]http.Request, 0)
	s := httptest.NewServer(prepareRegistryWithRecorder(&recordedRequests))
	defer s.Close()

	tempDir, opts := optionsForTesting(t)

	ref1 := refOnServer(s.URL, "test-vm:1.0")
	hash1 := makeTestVMWithContent(t, tempDir, ref1, "testvm123456789")
	err := Push(ref1, opts...)
	assert.NoError(t, err)
	deleteTestVMAt(t, tempDir, ref1)

	ref2 := refOnServer(s.URL, "test-vm:2.0")
	hash2 := makeTestVMWithContent(t, tempDir, ref2, "testvm123456789appendix")
	err = Push(ref2, opts...)
	assert.NoError(t, err)
	deleteTestVMAt(t, tempDir, ref2)

	err = Pull(ref2, opts...)
	require.NoError(t, err)
	err = Pull(ref1, opts...)

	hash1After := hashFromFile(t, filepath.Join(tempDir, "images", ref1, "disk.img"))
	hash2After := hashFromFile(t, filepath.Join(tempDir, "images", ref2, "disk.img"))

	assert.Equal(t, hash1, hash1After)
	assert.Equal(t, hash2, hash2After)
}
