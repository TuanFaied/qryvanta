import { LogLevel, Theme, type AppInfo, type Config } from "@/services/backend";

/** Mirrors config.Default() in Go. */
export function testConfig(): Config {
  return {
    schemaVersion: 1,
    appearance: { theme: Theme.ThemeDark, editorFontSize: 14 },
    execution: {
      readOnly: true,
      timeoutSeconds: 30,
      maxRows: 10000,
      maxResultMB: 64,
      probeTimeoutSeconds: 15,
      maxProbesPerRun: 10,
      connectTimeoutSeconds: 15,
    },
    history: { enabled: true, retentionDays: 30, storeResultSamples: false },
    logging: { level: LogLevel.LogInfo, logSqlText: false },
  };
}

export function testInfo(): AppInfo {
  return {
    version: { version: "0.1.0-test", commit: "abc123", goVersion: "go1.26", platform: "windows/amd64" },
    configPath: "C:\\Users\\dev\\AppData\\Roaming\\Qryvanta\\config.json",
    logPath: "C:\\Users\\dev\\AppData\\Local\\Qryvanta\\logs\\qryvanta.log",
    startupWarnings: [],
  };
}
