package protocol

const Version = "control.v1"

const (
	TypeEnrollRequest            = "enroll_request"
	TypeEnrollPending            = "enroll_pending"
	TypeEnrollCertificate        = "enroll_certificate"
	TypeHello                    = "hello"
	TypeWelcome                  = "welcome"
	TypeHeartbeat                = "heartbeat"
	TypeHeartbeatAck             = "heartbeat_ack"
	TypeInventoryReport          = "inventory_report"
	TypePressureReport           = "pressure_report"
	TypeSyncTask                 = "sync_task"
	TypeSyncTaskAck              = "sync_task_ack"
	TypeSyncTaskProgress         = "sync_task_progress"
	TypeSyncTaskResult           = "sync_task_result"
	TypeTrafficEvent             = "traffic_event"
	TypeTrafficEventAck          = "traffic_event_ack"
	TypeDownloadAuthorization    = "download_authorization"
	TypeDownloadAuthorizationAck = "download_authorization_ack"
	TypeAuthorizationStatusEvent = "authorization_status_event"
	TypeAuthorizationStatusAck   = "authorization_status_event_ack"
	TypeTrafficReplay            = "traffic_replay_request"
	TypeReconcileRequest         = "inventory_reconcile_request"
	TypeReconcileResult          = "inventory_reconcile_result"
	TypeNodeDisabled             = "node_disabled"
	TypeCertificateReject        = "certificate_rejected"
	TypeProtocolError            = "protocol_error"
)

var enrollmentTypes = map[string]bool{
	TypeEnrollRequest: true, TypeEnrollPending: true,
	TypeEnrollCertificate: true, TypeProtocolError: true,
}

var controlTypes = map[string]bool{
	TypeHello: true, TypeWelcome: true, TypeHeartbeat: true,
	TypeHeartbeatAck: true, TypeInventoryReport: true,
	TypePressureReport: true, TypeNodeDisabled: true,
	TypeSyncTask: true, TypeSyncTaskAck: true,
	TypeSyncTaskProgress: true, TypeSyncTaskResult: true,
	TypeTrafficEvent: true, TypeTrafficEventAck: true,
	TypeDownloadAuthorization: true, TypeDownloadAuthorizationAck: true,
	TypeAuthorizationStatusEvent: true, TypeAuthorizationStatusAck: true,
	TypeTrafficReplay:    true,
	TypeReconcileRequest: true, TypeReconcileResult: true,
	TypeCertificateReject: true, TypeProtocolError: true,
}

func AllowedOnEnrollment(messageType string) bool { return enrollmentTypes[messageType] }
func AllowedOnControl(messageType string) bool    { return controlTypes[messageType] }
