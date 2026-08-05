-- +goose Up

CREATE TABLE query_saved_statement_parameters (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  statement_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(64) NOT NULL,
  type VARCHAR(16) NOT NULL,
  ordinal SMALLINT UNSIGNED NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_saved_statement_parameter_name (statement_id, name),
  KEY idx_saved_statement_parameter_order (statement_id, ordinal),
  CONSTRAINT chk_saved_statement_parameter_type CHECK (type IN ('string', 'integer', 'decimal', 'boolean'))
);

-- +goose Down

DROP TABLE IF EXISTS query_saved_statement_parameters;
