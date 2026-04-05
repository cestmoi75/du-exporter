package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
)

func TestScanFolderMetrics(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := t.TempDir()
	logger := zap.NewNop()

	// Create test folder structure:
	// tmpDir/
	//   folder1/
	//     file1.txt (10 bytes)
	//     subfolder1/
	//       file2.txt (20 bytes)
	//   folder2/
	//     file3.txt (30 bytes)

	folder1 := filepath.Join(tmpDir, "folder1")
	folder2 := filepath.Join(tmpDir, "folder2")
	subfolder1 := filepath.Join(folder1, "subfolder1")

	os.MkdirAll(subfolder1, 0755)
	os.MkdirAll(folder2, 0755)

	// Create test files
	os.WriteFile(filepath.Join(folder1, "file1.txt"), []byte("0123456789"), 0644)                    // 10 bytes
	os.WriteFile(filepath.Join(subfolder1, "file2.txt"), []byte("01234567890123456789"), 0644)       // 20 bytes
	os.WriteFile(filepath.Join(folder2, "file3.txt"), []byte("012345678901234567890123456789"), 0644)   // 30 bytes

	// Scan the directory
	ScanFolder(tmpDir, []string{}, 2, logger)

	// Test metrics
	testCases := []struct {
		name        string
		metricName  string
		labelValues map[string]string
		expectedVal float64
	}{
		{
			name:       "folder1 file count at depth 1",
			metricName: "du_subfolder_file_count",
			labelValues: map[string]string{
				"folder":      "folder1",
				"parent_path": "/",
				"depth":       "1",
			},
			expectedVal: 1,
		},
		{
			name:       "folder1 size at depth 1",
			metricName: "du_folder_size_bytes",
			labelValues: map[string]string{
				"folder":      "folder1",
				"parent_path": "/",
				"depth":       "1",
			},
			expectedVal: 30, // 10 (file1) + 20 (file2 in subfolder1)
		},
		{
			name:       "folder2 file count at depth 1",
			metricName: "du_subfolder_file_count",
			labelValues: map[string]string{
				"folder":      "folder2",
				"parent_path": "/",
				"depth":       "1",
			},
			expectedVal: 1,
		},
		{
			name:       "folder2 size at depth 1",
			metricName: "du_folder_size_bytes",
			labelValues: map[string]string{
				"folder":      "folder2",
				"parent_path": "/",
				"depth":       "1",
			},
			expectedVal: 30,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			val := getMetricValueByName(t, tc.metricName, tc.labelValues)
			if val != tc.expectedVal {
				t.Errorf("Expected %v, got %v", tc.expectedVal, val)
			}
		})
	}
}

func TestScanFolderWithMaxDepth(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()

	// Create nested structure
	subfolder1 := filepath.Join(tmpDir, "folder1", "sub1")
	subfolder2 := filepath.Join(subfolder1, "sub2")
	os.MkdirAll(subfolder2, 0755)

	os.WriteFile(filepath.Join(tmpDir, "folder1", "file1.txt"), []byte("10bytes!!"), 0644)
	os.WriteFile(filepath.Join(subfolder1, "file2.txt"), []byte("20bytes!!!"), 0644)
	os.WriteFile(filepath.Join(subfolder2, "file3.txt"), []byte("30bytes!!"), 0644)

	// Scan with maxDepth=1
	ScanFolder(tmpDir, []string{}, 1, logger)

	// At depth 1, only folder1 should have metrics
	val := getMetricValueByName(t, "du_subfolder_file_count", map[string]string{
		"folder":      "folder1",
		"parent_path": "/",
		"depth":       "1",
	})

	if val != 1 {
		t.Errorf("Expected 1 file at depth 1, got %v", val)
	}

	// At depth 2, sub1 should have metrics (if depth limit wasn't applied)
	// Note: Since maxDepth=1, sub1 metrics shouldn't be registered
}

func TestParentPathLabel(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zap.NewNop()

	// Create nested structure with specific depth
	subfolder1 := filepath.Join(tmpDir, "folder1", "sub1", "sub2")
	os.MkdirAll(subfolder1, 0755)
	os.WriteFile(filepath.Join(tmpDir, "folder1", "file1.txt"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(subfolder1, "file2.txt"), []byte("test"), 0644)

	// Scan with maxDepth=3
	ScanFolder(tmpDir, []string{}, 3, logger)

	// Verify parent_path is set correctly
	testCases := []struct {
		folder      string
		parent_path string
		depth       string
		description string
	}{
		{
			folder:      "folder1",
			parent_path: "/",
			depth:       "1",
			description: "depth 1 should have parent_path=/",
		},
		{
			folder:      "sub1",
			parent_path: "/folder1",
			depth:       "2",
			description: "depth 2 should have parent_path=/folder1",
		},
		{
			folder:      "sub2",
			parent_path: "/folder1/sub1",
			depth:       "3",
			description: "depth 3 should have parent_path=/folder1/sub1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			val := getMetricValueByName(t, "du_folder_size_bytes", map[string]string{
				"folder":      tc.folder,
				"parent_path": tc.parent_path,
				"depth":       tc.depth,
			})
			if val == 0 {
				t.Errorf("Expected non-zero metric value for %s", tc.description)
			}
		})
	}
}

// Helper function to get metric value by metric name and labels
func getMetricValueByName(t *testing.T, metricName string, labels map[string]string) float64 {
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	for _, mf := range metrics {
		if mf.GetName() == metricName {
			for _, m := range mf.Metric {
				if matchLabels(m, labels) {
					if m.Gauge != nil {
						return *m.Gauge.Value
					} else if m.Counter != nil {
						return *m.Counter.Value
					}
				}
			}
		}
	}
	return 0
}

func matchLabels(metric *io_prometheus_client.Metric, expectedLabels map[string]string) bool {
	if len(metric.Label) != len(expectedLabels) {
		return false
	}

	for _, label := range metric.Label {
		expected, ok := expectedLabels[label.GetName()]
		if !ok || label.GetValue() != expected {
			return false
		}
	}
	return true
}
