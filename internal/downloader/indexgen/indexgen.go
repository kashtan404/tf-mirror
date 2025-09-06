package indexgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/sumdb/dirhash"
)

// IndexJSON is the root structure for minimal index.json
type IndexJSON struct {
	Versions map[string]struct{} `json:"versions"`
}

type VersionInfo struct {
	Platforms map[string]PlatformInfo `json:"platforms"`
}

type PlatformInfo struct {
	Filename string `json:"filename"`
}

// GenerateIndexJSON scans the provider directory and generates minimal index.json
// providerDir: path to .../registry.terraform.io/<namespace>/<name>
func GenerateIndexJSON(providerDir string) error {
	entries, err := os.ReadDir(providerDir)
	if err != nil {
		return fmt.Errorf("failed to read provider dir: %w", err)
	}

	index := IndexJSON{Versions: map[string]struct{}{}}

	// Find all provider archives and extract versions from filenames
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Example: terraform-provider-<name>_<version>_<os>_<arch>.zip
		if strings.HasPrefix(name, "terraform-provider-") && strings.HasSuffix(name, ".zip") {
			base := strings.TrimPrefix(name, "terraform-provider-")
			base = strings.TrimSuffix(base, ".zip")
			parts := strings.Split(base, "_")
			if len(parts) >= 4 {
				version := parts[1]
				platform := parts[2]
				arch := parts[3]
				index.Versions[version] = struct{}{}

				// Вычисляем хеш
				hash, err := calculateHash(filepath.Join(providerDir, name))
				if err != nil {
					return err
				}

				// Определяем путь для <version>.json
				indexPath := filepath.Join(providerDir, version+".json")

				// Читаем существующий индекс или создаем новый
				var indexFile map[string]any
				if data, err := os.ReadFile(indexPath); err == nil {
					json.Unmarshal(data, &indexFile)
				} else {
					indexFile = make(map[string]any)
					indexFile["archives"] = make(map[string]any)
				}

				// Получаем или создаем archives
				archives, exists := indexFile["archives"].(map[string]any)
				if !exists {
					archives = make(map[string]any)
					indexFile["archives"] = archives
				}

				// Добавляем информацию о файле
				fileName := filepath.Base(name)
				archives[platform+"_"+arch] = map[string]any{
					"hashes": []string{hash},
					"url":    fmt.Sprintf("%s", fileName),
				}

				// Сохраняем обновленный индекс
				saveIndex(indexPath, indexFile)
			}
		}
	}

	// Write index.json
	outPath := filepath.Join(providerDir, "index.json")
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("failed to create index.json: %w", err)
	}
	defer outFile.Close()
	enc := json.NewEncoder(outFile)
	enc.SetIndent("", "  ")
	if err := enc.Encode(index); err != nil {
		return fmt.Errorf("failed to encode index.json: %w", err)
	}
	return nil
}

// calculateHash вычисляет хеш файла, все как в исходниках terraform
// https://github.com/hashicorp/terraform/blob/main/internal/getproviders/hash.go#L296
func calculateHash(filePath string) (string, error) {
	archivePath, err := filepath.EvalSymlinks(string(filePath))

	// Используем HashZip для вычисления хеша
	hash, err := dirhash.HashZip(archivePath, dirhash.Hash1)
	if err != nil {
		return "", err
	}

	return hash, nil
}

// saveIndex сохраняет индекс в файл
func saveIndex(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
