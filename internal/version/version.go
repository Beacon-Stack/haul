package version

// AppName is the single source of truth for the application name.
// Used for binary name, config directory, user-agent, Pulse registration,
// and log prefix. Change this one constant to rename the app.
const AppName = "haul"

// Version is set at build time via -ldflags.
var Version = "dev"

// BuildTime is the UTC build timestamp, set at build time via -ldflags.
var BuildTime = "unknown"
