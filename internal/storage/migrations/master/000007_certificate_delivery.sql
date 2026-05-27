ALTER TABLE node_enrollment_requests ADD COLUMN node_id TEXT;
ALTER TABLE node_enrollment_requests ADD COLUMN certificate_id TEXT;
ALTER TABLE node_certificates ADD COLUMN certificate_pem TEXT;
ALTER TABLE node_certificates ADD COLUMN ca_chain_pem TEXT;
