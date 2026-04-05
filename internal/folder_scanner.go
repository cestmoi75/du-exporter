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

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subfolder := filepath.Join(root, entry.Name())
		if isExcluded(root, subfolder, excludeDirs) {
			continue
		}

		scanSubfolder(subfolder, entry.Name(), 1, maxDepth, logger)
	}

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
	var folderTotalSize int64
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
			folderTotalSize += subSize
		} else {
			info, err := entry.Info()
			if err != nil {
				logger.Error("Error getting file info", zap.String("path", filepath.Join(fullPath, entry.Name())), zap.Error(err))
				continue
			}
			totalCount++
			fileSize := info.Size()
			folderTotalSize += fileSize
			mtime := info.ModTime().Unix()
			if newest == 0 || mtime > newest {
				newest = mtime
			}
			if oldest == 0 || mtime < oldest {
				oldest = mtime
			}
		}
	}

	// 메트릭 저장
	// depth == 1일 때만 totalSize에 저장 (최상위 폴더 사이즈)
	if depth == 1 {
		totalSize.WithLabelValues(relPath).Set(float64(folderTotalSize))
	}
	// depth 제한 내에서만 folderSize에 저장
	if depth <= maxDepth {
		fileCount.WithLabelValues(relPath, strconv.Itoa(depth)).Set(float64(totalCount))
		folderSize.WithLabelValues(relPath, strconv.Itoa(depth)).Set(float64(folderTotalSize))
		if totalCount > 0 {
			newestMTime.WithLabelValues(relPath, strconv.Itoa(depth)).Set(float64(newest))
			oldestMTime.WithLabelValues(relPath, strconv.Itoa(depth)).Set(float64(oldest))
		}
	}

	return folderTotalSize
}
