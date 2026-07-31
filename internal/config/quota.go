package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

type Quota struct {
	RequestBuckets                  RequestBuckets  `yaml:"request_buckets"`
	PublicResourceBuckets           RequestBuckets  `yaml:"public_resource_buckets"`
	DailyTraffic                    DailyTraffic    `yaml:"daily_traffic"`
	AuthorizationMaxBytesMultiplier int             `yaml:"authorization_max_bytes_multiplier"`
	RangeConcurrencyLimit           int             `yaml:"range_concurrency_limit"`
	Blacklist                       []string        `yaml:"blacklist"`
	Blocklist                       Blocklist       `yaml:"blocklist"`
	AbuseControl                    AbuseControl    `yaml:"abuse_control"`
	ChallengeLimits                 ChallengeLimits `yaml:"challenge_limits"`
	Exemptions                      []string        `yaml:"exemptions"`
}

type ChallengeLimits struct {
	BucketCapacity      int    `yaml:"bucket_capacity"`
	BucketFullRefill    string `yaml:"bucket_full_refill"`
	MaxOutstandingExact int    `yaml:"max_outstanding_exact"`
	MaxOutstandingTotal int    `yaml:"max_outstanding_total"`
}

type Blocklist struct {
	Static          []string        `yaml:"static"`
	Feeds           []BlocklistFeed `yaml:"feeds"`
	AutoBanDuration string          `yaml:"auto_ban_duration"`
}

type BlocklistFeed struct {
	URL             string `yaml:"url"`
	RefreshInterval string `yaml:"refresh_interval"`
	Timeout         string `yaml:"timeout"`
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
	legacyBlacklist := false
	data, repaired, err := readYAMLWithRepair(path, &c, QuotaExample, QuotaRepairExample,
		func(doc *yaml.Node) bool {
			changed, found := migrateQuotaBlacklist(doc)
			legacyBlacklist = legacyBlacklist || found
			return changed
		})
	if err != nil {
		return c, err
	}
	if legacyBlacklist {
		warnDeprecated(warn, "quota.blacklist", "quota.blocklist.static")
	}
	if len(c.Blacklist) > 0 {
		c.Blocklist.Static = append(c.Blacklist, c.Blocklist.Static...)
		c.Blacklist = nil
	}
	defaultBucket(&c.RequestBuckets.IPv432, 120, "48h", "request_buckets.ipv4_32", warn)
	defaultBucket(&c.RequestBuckets.IPv424, 600, "48h", "request_buckets.ipv4_24", warn)
	defaultBucket(&c.RequestBuckets.IPv6128, 120, "48h", "request_buckets.ipv6_128", warn)
	defaultBucket(&c.RequestBuckets.IPv664, 600, "48h", "request_buckets.ipv6_64", warn)
	defaultBucket(&c.PublicResourceBuckets.IPv432, 3600, "1h", "public_resource_buckets.ipv4_32", warn)
	defaultBucket(&c.PublicResourceBuckets.IPv424, 10800, "1h", "public_resource_buckets.ipv4_24", warn)
	defaultBucket(&c.PublicResourceBuckets.IPv6128, 3600, "1h", "public_resource_buckets.ipv6_128", warn)
	defaultBucket(&c.PublicResourceBuckets.IPv664, 10800, "1h", "public_resource_buckets.ipv6_64", warn)
	setString(&c.DailyTraffic.IPv432, "3 GiB", "daily_traffic.ipv4_32", warn)
	setString(&c.DailyTraffic.IPv424, "20 GiB", "daily_traffic.ipv4_24", warn)
	setString(&c.DailyTraffic.IPv6128, "3 GiB", "daily_traffic.ipv6_128", warn)
	setString(&c.DailyTraffic.IPv664, "20 GiB", "daily_traffic.ipv6_64", warn)
	if c.AuthorizationMaxBytesMultiplier == 0 {
		c.AuthorizationMaxBytesMultiplier = 2
		warnDefault(warn, "authorization_max_bytes_multiplier", "2")
	}
	if c.RangeConcurrencyLimit == 0 {
		c.RangeConcurrencyLimit = 32
		warnDefault(warn, "range_concurrency_limit", "32")
	}
	setString(&c.Blocklist.AutoBanDuration, "168h", "blocklist.auto_ban_duration", warn)
	if c.ChallengeLimits.BucketCapacity == 0 {
		c.ChallengeLimits.BucketCapacity = 30
		warnDefault(warn, "challenge_limits.bucket_capacity", "30")
	}
	setString(&c.ChallengeLimits.BucketFullRefill, "10m", "challenge_limits.bucket_full_refill", warn)
	if c.ChallengeLimits.MaxOutstandingExact == 0 {
		c.ChallengeLimits.MaxOutstandingExact = 4
		warnDefault(warn, "challenge_limits.max_outstanding_exact", "4")
	}
	if c.ChallengeLimits.MaxOutstandingTotal == 0 {
		c.ChallengeLimits.MaxOutstandingTotal = 100000
		warnDefault(warn, "challenge_limits.max_outstanding_total", "100000")
	}
	applyAbuseControlDefaults(&c.AbuseControl, warn)
	if err := validateQuota(c); err != nil {
		return c, err
	}
	return c, writeRepairedYAML(path, data, repaired)
}

func defaultBucket(bucket *Bucket, capacity int, refill, field string, warn WarnFunc) {
	if bucket.Capacity == 0 {
		bucket.Capacity = capacity
		warnDefault(warn, field+".capacity", stringValue(capacity))
	}
	setString(&bucket.FullRefill, refill, field+".full_refill", warn)
}

func stringValue(value int) string {
	return fmt.Sprintf("%d", value)
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
	for field, bucket := range map[string]Bucket{"ipv4_32": c.PublicResourceBuckets.IPv432, "ipv4_24": c.PublicResourceBuckets.IPv424, "ipv6_128": c.PublicResourceBuckets.IPv6128, "ipv6_64": c.PublicResourceBuckets.IPv664} {
		if bucket.Capacity <= 0 {
			return errors.New("公共资源请求额度桶容量必须大于零")
		}
		if err := validDuration("public_resource_buckets."+field+".full_refill", bucket.FullRefill); err != nil {
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
	if c.RangeConcurrencyLimit <= 0 {
		return errors.New("range_concurrency_limit 必须大于零")
	}
	for i, raw := range c.Exemptions {
		if _, err := parseQuotaPrefix(raw); err != nil {
			return fmt.Errorf("quota.exemptions[%d]: %w", i, err)
		}
	}
	for i, raw := range c.Blacklist {
		if _, err := parseQuotaPrefix(raw); err != nil {
			return fmt.Errorf("quota.blacklist[%d]: %w", i, err)
		}
	}
	for i, raw := range c.Blocklist.Static {
		if _, err := parseQuotaPrefix(raw); err != nil {
			return fmt.Errorf("quota.blocklist.static[%d]: %w", i, err)
		}
	}
	for i, feed := range c.Blocklist.Feeds {
		if err := validateBlocklistFeed(i, feed); err != nil {
			return err
		}
	}
	if err := validDuration("quota.blocklist.auto_ban_duration", c.Blocklist.AutoBanDuration); err != nil {
		return err
	}
	if err := validateAbuseControl(c.AbuseControl); err != nil {
		return err
	}
	if c.ChallengeLimits.BucketCapacity <= 0 || c.ChallengeLimits.MaxOutstandingExact <= 0 ||
		c.ChallengeLimits.MaxOutstandingTotal <= 0 {
		return errors.New("challenge_limits 的所有限制值必须大于零")
	}
	if err := validDuration("challenge_limits.bucket_full_refill", c.ChallengeLimits.BucketFullRefill); err != nil {
		return err
	}
	return nil
}

func validateBlocklistFeed(index int, feed BlocklistFeed) error {
	parsed, err := url.Parse(strings.TrimSpace(feed.URL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("quota.blocklist.feeds[%d].url 必须是 http/https URL", index)
	}
	if feed.RefreshInterval != "" {
		if err := validDuration(fmt.Sprintf("quota.blocklist.feeds[%d].refresh_interval", index),
			feed.RefreshInterval); err != nil {
			return err
		}
	}
	if feed.Timeout != "" {
		if err := validDuration(fmt.Sprintf("quota.blocklist.feeds[%d].timeout", index),
			feed.Timeout); err != nil {
			return err
		}
	}
	return nil
}

func parseQuotaPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, errors.New("空白 IP/CIDR 项无效")
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
