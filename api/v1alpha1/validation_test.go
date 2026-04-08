package v1alpha1

import "testing"

func TestValidateMySQLDriverConfigRejectsMixedDatabaseAndTableScope(t *testing.T) {
	err := validateMySQLDriverConfig(&MySQLDriverConfig{
		Databases: []string{"orders"},
		Tables:    []string{"orders.items"},
	})
	if err == nil {
		t.Fatalf("expected mixed databases/tables config to be rejected")
	}
}

func TestValidateMySQLDriverConfigRejectsInvalidTableSelector(t *testing.T) {
	err := validateMySQLDriverConfig(&MySQLDriverConfig{
		Tables: []string{"orders_only"},
	})
	if err == nil {
		t.Fatalf("expected invalid table selector to be rejected")
	}
}

func TestValidateMySQLDriverConfigAcceptsSupportedRestoreModes(t *testing.T) {
	for _, mode := range []string{"merge", "wipe-all-user-databases"} {
		if err := validateMySQLDriverConfig(&MySQLDriverConfig{RestoreMode: mode}); err != nil {
			t.Fatalf("expected restore mode %q to be accepted: %v", mode, err)
		}
	}
}

func TestValidateRedisDriverConfigAcceptsRDBMode(t *testing.T) {
	if err := validateRedisDriverConfig(&RedisDriverConfig{Mode: "rdb"}); err != nil {
		t.Fatalf("expected redis rdb mode to be accepted: %v", err)
	}
}

func TestValidateRedisDriverConfigRejectsUnsupportedMode(t *testing.T) {
	if err := validateRedisDriverConfig(&RedisDriverConfig{Mode: "aof"}); err == nil {
		t.Fatalf("expected unsupported redis mode to be rejected")
	}
}

func TestValidateMinIODriverConfigRejectsVersionedBackup(t *testing.T) {
	if err := validateMinIODriverConfig(&MinIODriverConfig{IncludeVersions: true}); err == nil {
		t.Fatalf("expected includeVersions=true to be rejected until the built-in addon supports it")
	}
}
