package protocol

const Version = "control.v1"

const (
	TypeEnrollRequest     = "enroll_request"
	TypeEnrollPending     = "enroll_pending"
	TypeEnrollCertificate = "enroll_certificate"
	TypeHello             = "hello"
	TypeWelcome           = "welcome"
	TypeHeartbeat         = "heartbeat"
	TypeHeartbeatAck      = "heartbeat_ack"
	TypeInventoryReport   = "inventory_report"
	TypePressureReport    = "pressure_report"
	TypeNodeDisabled      = "node_disabled"
	TypeCertificateReject = "certificate_rejected"
	TypeProtocolError     = "protocol_error"
)

var enrollmentTypes = map[string]bool{
	TypeEnrollRequest: true, TypeEnrollPending: true,
	TypeEnrollCertificate: true, TypeProtocolError: true,
}

var controlTypes = map[string]bool{
	TypeHello: true, TypeWelcome: true, TypeHeartbeat: true,
	TypeHeartbeatAck: true, TypeInventoryReport: true,
	TypePressureReport: true, TypeNodeDisabled: true,
	TypeCertificateReject: true, TypeProtocolError: true,
}

func AllowedOnEnrollment(messageType string) bool { return enrollmentTypes[messageType] }
func AllowedOnControl(messageType string) bool    { return controlTypes[messageType] }
