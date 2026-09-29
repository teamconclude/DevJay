#ifndef DEVJAY_AGENT_PROCESSES_DARWIN_H
#define DEVJAY_AGENT_PROCESSES_DARWIN_H

// Background-only, read-only native process metadata. The caller frees the
// returned JSON array; no process arguments, environment, or contents are read.
char *devjay_agent_processes(void);
// Refresh only previously identified processes; reject changed identity/host/cwd.
char *devjay_refresh_agent_processes(const char *knownJSON);

#endif
