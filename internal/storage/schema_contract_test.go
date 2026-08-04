package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var expectedSchemaFiles = map[string]string{
	"migrations/master/000001_core.sql":                     "17801e5c80bc8446393d034147d934b92c79faa631d2ac1eb702ef704f91bc1f",
	"migrations/master/000002_inventory.sql":                "d479f4ec2f04ba52fe374f7153d3d37dfbf3195adaade650b6c47622e681b43d",
	"migrations/master/000003_operations.sql":               "a27660821da8487bd0bc8c2614ab8ffca35f8e0cb5b745e679492d2c74e7679b",
	"migrations/master/000004_accounting.sql":               "33517fd69e0b057e84bd8edeae9d2889a0991adca9c2550b59d2e4f568c2d148",
	"migrations/master/000005_control_plane.sql":            "c0eba577110c1c3f969bcc60e6f754379f630386e22fa9c30c536454b2647546",
	"migrations/master/000006_control_runtime.sql":          "d4ae0b9763d3b97025557b922f4a53175b58171668cf4c227a8f21d5d84b6af8",
	"migrations/master/000007_certificate_delivery.sql":     "86d9baca856ef60f8f4a393915763a06ef3c3b77e4ec6a69ba394adc0aca4369",
	"migrations/master/000008_mirror_sync.sql":              "1ae4872b66ff4ac56a17ec7633fa0418d7887598dfa762bfe9266d5176854b2d",
	"migrations/master/000009_accounting_m5.sql":            "1afd83a3162c7830e5a79216efecf6fa9924f6b058c77aa3f8c058eb184c0490",
	"migrations/master/000010_public_download_url.sql":      "723ce1f25cc73154694d9b953e2af5607a6181f457e5f0d886a9d7cf8db6006e",
	"migrations/master/000011_public_stats_rollups.sql":     "39e825f2f5a2b1d91f064a905ef323382653ffb427768df37e0842a1614fc0e2",
	"migrations/master/000012_asset_system.sql":             "1dec68152518b6c1214f736adff28361399304771063555c9647262e338e4035",
	"migrations/master/000013_project_scan_state.sql":       "b7d222d30c2b9f92d7334fb7f91d9f88611af8859533c4fcc3a9fbd39bd1bfd6",
	"migrations/master/000014_client_blocks.sql":            "3c1e08facd52c7192b1ae3d37caab8ee6b89e5b2ba46384cbfa306caf05f320d",
	"migrations/master/000015_admin_web.sql":                "c69b778d51ecd8ec2b0459e080bd5137baac7f44ba69e3b10e271c6440080aa0",
	"migrations/master/000016_sync_task_lease.sql":          "6e2f9c4b2ecdfc6e9d410e5d15eaafd23b3a13d6c1249f6052266c15caed9705",
	"migrations/master/000017_node_project_assignments.sql": "c68146df64987598b8378085f1aa0c51345ddb5a83cef2c2ce10ec4baf107eba",
	"migrations/master/000018_public_probe.sql":             "700a6fa0ceb434b0f9608fbf2bf4e590c88c82718bf156349b3757f8033bbffd",
	"migrations/master/000019_admin_block_display_ip.sql":   "fa7885e78b7aec3572d71b39f988304b46f5ea3f5bbee9aa0fcb40a663f17936",
	"migrations/master/000020_asset_classification.sql":     "98fdc26f2d792f8b1877708265d8804a77ebf3c0a7a40f2ef12fb4677895c8cb",
	"migrations/master/000021_node_download_priority.sql":   "91ddf909b375e8bee6f53bd5ff86a861ac3d9a6b8ad0f25721dfa11b15bc499d",
	"migrations/master/000022_authorization_status.sql":     "f5f7084ffa78c9ed956fff8c7ae90c311a59e9f08d593bb6799003e81d589b3e",
	"migrations/master/000023_opaque_download_tokens.sql":   "94a34d6ad2e8ad2fcb9c8964931322db53114ec5cab3c7e43fe4be0a3a375689",
	"migrations/master/000024_public_stats_indexes.sql":     "3e336a71d7e4833e78635e32477a6b64bd2a5bf19cfe6449a227a122d4671e18",
	"migrations/master/000025_download_source_stats.sql":    "32017e1724a47cf70d982e933aa5443c962385969eec4df4d3fec3b77cf51f8c",
	"migrations/master/000026_public_stats_state.sql":       "b2e6188912068338b9565ce7e1a26651a8d30cdb132856a2f592b28a751ad990",
	"migrations/master/000027_project_stats_state.sql":      "41f9bb8369a38929a6a02c8761de90f251c3cd6dbac6bdb6fb70c76f31ffad61",
	"migrations/master/000028_availability_rollups.sql":     "a31dcef16f335ece42cd439a2e83374af4c590cbdde7b948dd1eb816e0558332",
	"migrations/master/000029_target_reconcile_indexes.sql": "4f43e95ea7d18e1a31128d11a583a788a14f4466f1ba890fc969fb04e742bf51",
	"migrations/master/000030_client_blocks_abuse.sql":      "b5965f12978b916bd72fb38bcf15f34b19352248dc163881921215441f43d12e",
	"migrations/master/000031_node_region.sql":              "f7396fb2190b3af910bf2dbd0bc8697f821781db1309e7e93842ce65b2dbe11d",
	"migrations/node/000001_state.sql":                      "3501b8a94e3fac8eb0807afd32183effd81874e2001ff7b69cb43b0a25b3a0c2",
	"migrations/node/000002_identity.sql":                   "dab9fe79b6b455ce7109c7eac041e17732ad5d0c2eb7f9c89bf3f793dd30b88e",
	"migrations/node/000003_sync_state.sql":                 "973fb3190ee2a212b883e3d6686db18afdcb3bb1fdd59d719353ec0cf99d77e7",
	"migrations/node/000004_sync_results.sql":               "4f323aaf9e08f016c8f35b1a139b44f3349e55a192503bd1bdb9a7a8145308ac",
	"migrations/node/000004_traffic_events_m5.sql":          "255d1a5e45884f4c7ebd3efe6800b85295af95aa63a67e6a15e6368f234f8925",
	"migrations/node/000005_identity_materials.sql":         "c083a2ead0e7c1046a952661597ce2612df66ca78407ce482ff1aaf6199dfdcb",
	"migrations/node/000006_inventory_force_report.sql":     "78b0bab767831725fd5b90fdf781095b0edf42d0e1a9bf56f30ba09852c432a3",
	"migrations/node/000007_authorization_state.sql":        "091deeac4f7ef4a1196ff0f55af72b9b19eb064a35ca9e1d8235fc08f2d7743c",
	"migrations/node/000008_opaque_download_tokens.sql":     "93376fc051fc9c73f5309aadcddd8860793ebf679b3b067f38f0835c4a8c0571",
}

func TestSchemaFilesRequireVersionedUpgrade(t *testing.T) {
	names, err := fs.Glob(schemaFiles, "migrations/*/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if len(names) != len(expectedSchemaFiles) {
		t.Fatalf("数据库结构文件数量变化：got=%d want=%d；请提升数据库版本、添加逐级升级器并更新 docs/数据库版本升级.md",
			len(names), len(expectedSchemaFiles))
	}
	for _, name := range names {
		want, ok := expectedSchemaFiles[name]
		if !ok {
			t.Fatalf("发现未登记的数据库结构文件 %s；请提升数据库版本、添加逐级升级器并更新结构指纹", name)
		}
		data, err := schemaFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if got != want {
			t.Fatalf("数据库结构文件 %s 已变化；请提升数据库版本、添加逐级升级器并更新结构指纹 got=%s want=%s",
				name, got, want)
		}
	}
}

func TestDatabaseVersionsHaveDocs(t *testing.T) {
	for version := 1; version <= masterDBVersion; version++ {
		assertVersionDoc(t, "master", version)
	}
	for version := 1; version <= nodeDBVersion; version++ {
		assertVersionDoc(t, "node", version)
	}
}

func assertVersionDoc(t *testing.T, kind string, version int) {
	t.Helper()
	path := filepath.Join("..", "..", "docs", kind, "v"+itoa(version)+".md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("缺少数据库版本文档 %s；每升一级数据库版本都必须新增对应端文档", path)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
