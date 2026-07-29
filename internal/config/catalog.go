package config

import "errors"

func applyCatalogDefaults(server *MasterServer, warn WarnFunc) {
	if server.CatalogBatchRows == nil {
		value := 4
		server.CatalogBatchRows = &value
		warnDefault(warn, "server.catalog_batch_rows", "4")
	}
	if server.CatalogPrefetchRemainingRows == nil {
		value := 1
		server.CatalogPrefetchRemainingRows = &value
		warnDefault(warn, "server.catalog_prefetch_remaining_rows", "1")
	}
}

func validateCatalog(server MasterServer) error {
	if server.CatalogBatchRows == nil || *server.CatalogBatchRows <= 0 {
		return errors.New("配置字段 server.catalog_batch_rows 必须大于零")
	}
	if server.CatalogPrefetchRemainingRows == nil ||
		*server.CatalogPrefetchRemainingRows < 0 ||
		*server.CatalogPrefetchRemainingRows >= *server.CatalogBatchRows {
		return errors.New("配置字段 server.catalog_prefetch_remaining_rows 必须大于等于零且小于 catalog_batch_rows")
	}
	return nil
}
