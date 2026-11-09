-- 014_dispatch_proc_start.sql — when a dispatch's process started (#541).
--
-- Crash recovery decided whether a dispatch was still running only by
-- whether its PID was alive. After a crash and a reboot, or enough time, the
-- PID can belong to another process, and the task stayed in-progress
-- forever. proc_start is the process's start time as the OS reports it
-- (/proc/<pid>/stat's starttime on Linux, `ps -o lstart=` elsewhere), opaque
-- and only ever compared for equality; '' when it could not be read, which
-- keeps the PID-only check.
--
-- Go-only, like 006 to 013.
--
-- Compatibility: SQLite >= 3.32. No RETURNING, STRICT, or GENERATED.

PRAGMA user_version = 14;

ALTER TABLE dispatches ADD COLUMN proc_start TEXT NOT NULL DEFAULT '';
