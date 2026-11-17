/**
 * A background task's status as notifications read it; jhon-shell's
 * background sub-agents report these changes.
 */
export const TASK_STATUS = {
	QUEUED: "queued",
	RUNNING: "running",
	WAITING: "waiting",
	COMPLETED: "completed",
	FAILED: "failed",
	CANCELLED: "cancelled",
	TIMED_OUT: "timed_out",
} as const;

export type TaskStatus = (typeof TASK_STATUS)[keyof typeof TASK_STATUS];

export interface TaskStatusChange {
	readonly producerId: string;
	readonly sessionId: string;
	readonly parentSessionId: string;
	readonly taskId: string;
	readonly runId: string;
	readonly status: TaskStatus;
	readonly previousStatus?: TaskStatus;
	readonly sequence: number;
}
