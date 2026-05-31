package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

type Quota struct {
	RequestBuckets                  RequestBuckets `yaml:"request_buckets"`
	DailyTraffic                    DailyTraffic   `yaml:"daily_traffic"`
	AuthorizationMaxBytesMultiplier int            `yaml:"authorization_max_bytes_multiplier"`
	Blacklist                       []string       `yaml:"blacklist"`
	Exemptions                      []string       `yaml:"exemptions"`
}

type RequestBuckets struct {
	IPv432  Bucket `yaml:"ipv4_32"`
	IPv424  Bucket `yaml:"ipv4_24"`
	IPv6128 Bucket `yaml:"ipv6_128"`
	IPv664  Bucket `yaml:"ipv6_64"`
}

type Bucket struct {
	Capacity   int    `yaml:"capacity"`
	FullRefill string `yaml:"full_refill"`
}

type DailyTraffic struct {
	IPv432  string `yaml:"ipv4_32"`
	IPv424  string `yaml:"ipv4_24"`
	IPv6128 string `yaml:"ipv6_128"`
	IPv664  string `yaml:"ipv6_64"`
}

func LoadQuota(path string, warn WarnFunc) (Quota, error) {
	var c Quota
	if err := readYAML(path, &c, QuotaExample); err != nil {
		return c, err
	}
	defaultBucket(&c.RequestBuckets.IPv432, 120, "request_buckets.ipv4_32", warn)
	defaultBucket(&c.RequestBuckets.IPv424, 600, "request_buckets.ipv4_24", warn)
	defaultBucket(&c.RequestBuckets.IPv6128, 120, "request_buckets.ipv6_128", warn)
	defaultBucket(&c.RequestBuckets.IPv664, 600, "request_buckets.ipv6_64", warn)
	setString(&c.DailyTraffic.IPv432, "3 GiB", "daily_traffic.ipv4_32", warn)
	setString(&c.DailyTraffic.IPv424, "20 GiB", "daily_traffic.ipv4_24", warn)
	setString(&c.DailyTraffic.IPv6128, "3 GiB", "daily_traffic.ipv6_128", warn)
	setString(&c.DailyTraffic.IPv664, "20 GiB", "daily_traffic.ipv6_64", warn)
	if c.AuthorizationMaxBytesMultiplier == 0 {
		c.AuthorizationMaxBytesMultiplier = 2
		warnDefault(warn, "authorization_max_bytes_multiplier", "2")
	}
	return c, validateQuota(c)
}

func defaultBucket(bucket *Bucket, capacity int, field string, warn WarnFunc) {
	if bucket.Capacity == 0 {
		bucket.Capacity = capacity
		warnDefault(warn, field+".capacity", stringValue(capacity))
	}
	setString(&bucket.FullRefill, "48h", field+".full_refill", warn)
}

func stringValue(value int) string {
	if value == 120 {
		return "120"
	}
	return "600"
}

func validateQuota(c Quota) error {
	for field, bucket := range map[string]Bucket{"ipv4_32": c.RequestBuckets.IPv432, "ipv4_24": c.RequestBuckets.IPv424, "ipv6_128": c.RequestBuckets.IPv6128, "ipv6_64": c.RequestBuckets.IPv664} {
		if bucket.Capacity <= 0 {
			return errors.New("请求额度桶容量必须大于零")
		}
		if err := validDuration("request_buckets."+field+".full_refill", bucket.FullRefill); err != nil {
			return err
		}
	}
	for field, value := range map[string]string{"ipv4_32": c.DailyTraffic.IPv432, "ipv4_24": c.DailyTraffic.IPv424, "ipv6_128": c.DailyTraffic.IPv6128, "ipv6_64": c.DailyTraffic.IPv664} {
		if _, err := parseGiB("daily_traffic."+field, value); err != nil {
			return err
		}
	}
	if c.AuthorizationMaxBytesMultiplier <= 0 {
		return errors.New("authorization_max_bytes_multiplier 必须大于零")
	}
	for i, raw := range c.Exemptions {
		if _, err := parseQuotaPrefix(raw); err != nil {
			return fmt.Errorf("quota.exemptions[%d]: %w", i, err)
		}
	}
	return nil
}

func parseQuotaPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, errors.New("空白白名单项无效")
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Masked(), nil
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Prefix{}, err
	}
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32), nil
	}
	return netip.PrefixFrom(addr, 128), nil
}
