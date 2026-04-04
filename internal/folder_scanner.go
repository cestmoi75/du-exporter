package internal

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"go.uber.org/zap"
)

func ScanFolder(root string, excludeDirs []string, maxDepth int, logger *zap.Logger) {
	fileCount.Reset()
	folderSize.Reset()
	totalSize.Reset()
	newestMTime.Reset()
	oldestMTime.Reset()

	start := time.Now() // for scan duration

	entries, err := os.ReadDir(root)
	if err != nil {
		logger.Error("Error reading root folder", zap.String("root", root), zap.Error(err))
		scanErrors.Inc()
		return
	}

	var grandTotalSize int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subfolder := filepath.Join(root, entry.Name())
		if isExcluded(root, subfolder, excludeDirs) {
			continue
		}

		subSize := scanSubfolder(subfolder, entry.Name(), 1, maxDepth, logger)
		grandTotalSize += subSize
	}

	// 탑레벨 전체 사이즈를 totalSize 메트릭에 저장
	totalSize.WithLabelValues(filepath.Base(root)).Set(float64(grandTotalSize))

	scanDuration.Observe(time.Since(start).Seconds())
	scanCount.Inc()
}

func isExcluded(root, path string, excludes []string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)

	for _, ex := range excludes {
		if filepath.Clean(ex) == rel {
			return true
		}
	}
	return false
}

func scanSubfolder(fullPath string, relPath string, depth int, maxDepth int, logger *zap.Logger) int64 {
	var totalSize int64
	var totalCount int
	var newest, oldest int64

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		logger.Error("Error reading subfolder", zap.String("path", fullPath), zap.Error(err))
		scanErrors.Inc()
		return 0
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subRelPath := filepath.Join(relPath, entry.Name())
			subFullPath := filepath.Join(fullPath, entry.Name())
			subSize := scanSubfolder(subFullPath, subRelPath, depth+1, maxDepth, logger)
			totalSize += subSize
		} else {
			info, err := entry.Info()
			if err != nil {
				logger.Error("Error getting file info", zap.String("path", filepath.Join(fullPath, entry.Name())), zap.Error(err))
				continue
			}
			totalCount++
			fileSize := info.Size()
			totalSize += fileSize
			mtime := info.ModTime().Unix()
			if newest == 0 || mtime > newest {
				newest = mtime
			}
			if oldest == 0 || mtime < oldest {
				oldest = mtime
			}
		}
	}

	// 메트릭 저장 (depth 제한 내에서만)
	if depth <= maxDepth {
		fileCount.WithLabelValues(relPath).Set(float64(totalCount))
		folderSize.WithLabelValues(relPath, strconv.Itoa(depth)).Set(float64(totalSize))
		if totalCount > 0 {
			newestMTime.WithLabelValues(relPath).Set(float64(newest))
			oldestMTime.WithLabelValues(relPath).Set(float64(oldest))
		}
	}

	return totalSize
}
