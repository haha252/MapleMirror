package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

type cacheDocument struct {
	Version   int            `json:"version"`
	UpdatedAt string         `json:"updated_at"`
	Prefixes  []prefixRecord `json:"prefixes"`
}

const cacheDocumentVersion = 1

type prefixRecord struct {
	Prefix string `json:"prefix"`
	Region Region `json:"region"`
}

type countryDocument struct {
	CountryCode string `json:"countryCode"`
	Prefixes    struct {
		IPv4 []string `json:"ipv4"`
		IPv6 []string `json:"ipv6"`
	} `json:"prefixes"`
}

var countryCodePattern = regexp.MustCompile(`^[A-Za-z]{2}$`)

func parseArchive(data []byte) (cacheDocument, snapshot, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return cacheDocument{}, snapshot{}, err
	}
	defer reader.Close()
	tarReader := tar.NewReader(io.LimitReader(reader, maxArchiveBytes))
	var records []prefixRecord
	foundChina := false
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return cacheDocument{}, snapshot{}, err
		}
		code, ok := countryCodeFromPath(header.Name)
		if !ok || header.Typeflag != tar.TypeReg {
			continue
		}
		body, err := readLimited(tarReader, maxCountryFileBytes)
		if err != nil {
			return cacheDocument{}, snapshot{}, err
		}
		var country countryDocument
		if err := json.Unmarshal(body, &country); err != nil {
			return cacheDocument{}, snapshot{}, fmt.Errorf("国家 %s: %w", code, err)
		}
		countryCode := strings.ToUpper(strings.TrimSpace(country.CountryCode))
		if countryCode != "" && countryCode != code {
			return cacheDocument{}, snapshot{}, fmt.Errorf("国家目录 %s 与 countryCode %s 不一致", code, countryCode)
		}
		region := RegionOutsideMainland
		if code == "CN" {
			region = RegionMainlandChina
			foundChina = true
		}
		for _, raw := range country.Prefixes.IPv4 {
			records = append(records, prefixRecord{Prefix: raw, Region: region})
		}
		for _, raw := range country.Prefixes.IPv6 {
			records = append(records, prefixRecord{Prefix: raw, Region: region})
		}
	}
	if !foundChina {
		return cacheDocument{}, snapshot{}, errors.New("数据中缺少 CN 国家记录")
	}
	cleaned, snap, err := buildSnapshot(records)
	if err != nil {
		return cacheDocument{}, snapshot{}, err
	}
	return cacheDocument{Version: cacheDocumentVersion, Prefixes: cleaned}, snap, nil
}

func buildSnapshot(records []prefixRecord) ([]prefixRecord, snapshot, error) {
	if len(records) == 0 {
		return nil, snapshot{}, errors.New("国家 IP 数据库没有有效前缀")
	}
	v4 := prefixTree{root: &prefixNode{}}
	v6 := prefixTree{root: &prefixNode{}}
	seen := make(map[string]Region, len(records))
	cleaned := make([]prefixRecord, 0, len(records))
	for _, record := range records {
		region := record.Region
		if region != RegionMainlandChina && region != RegionOutsideMainland {
			return nil, snapshot{}, fmt.Errorf("前缀 %s 的地区无效：%s", record.Prefix, region)
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(record.Prefix))
		if err != nil {
			return nil, snapshot{}, fmt.Errorf("前缀 %s 无效：%w", record.Prefix, err)
		}
		prefix = prefix.Masked()
		key := prefix.String()
		if previous, ok := seen[key]; ok {
			if previous != region {
				return nil, snapshot{}, fmt.Errorf("前缀 %s 同时属于多个地区", key)
			}
			continue
		}
		seen[key] = region
		cleaned = append(cleaned, prefixRecord{Prefix: key, Region: region})
		if prefix.Addr().Is4() {
			if err := v4.insert(prefix, region); err != nil {
				return nil, snapshot{}, err
			}
		} else if err := v6.insert(prefix, region); err != nil {
			return nil, snapshot{}, err
		}
	}
	sort.Slice(cleaned, func(i, j int) bool {
		if cleaned[i].Prefix != cleaned[j].Prefix {
			return cleaned[i].Prefix < cleaned[j].Prefix
		}
		return cleaned[i].Region < cleaned[j].Region
	})
	return cleaned, snapshot{ipv4: v4, ipv6: v6}, nil
}

func countryCodeFromPath(name string) (string, bool) {
	parts := strings.Split(strings.Trim(name, "/"), "/")
	if len(parts) != 3 || parts[0] != "country" || parts[2] != "aggregated.json" {
		return "", false
	}
	code := strings.ToUpper(parts[1])
	return code, countryCodePattern.MatchString(code)
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("数据超过 %d 字节限制", limit)
	}
	return data, nil
}
