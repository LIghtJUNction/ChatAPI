package platform

type MigrationStatus struct {
	SchemaVersion  string             `json:"schema_version"`
	AppVersion     string             `json:"app_version,omitempty"`
	MigrationDirty bool               `json:"migration_dirty"`
	MigrationLock  string             `json:"migration_lock,omitempty"`
	CreatedBy      string             `json:"created_by,omitempty"`
	LastMigratedAt string             `json:"last_migrated_at,omitempty"`
	Applied        []AppliedMigration `json:"applied"`
}

type AppliedMigration struct {
	Version   string `json:"version"`
	Name      string `json:"name,omitempty"`
	AppliedAt string `json:"applied_at"`
	Checksum  string `json:"checksum,omitempty"`
	Dirty     bool   `json:"dirty"`
}
