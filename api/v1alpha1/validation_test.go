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
