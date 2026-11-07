import {
  AuthMethod,
  EncryptMode,
  KeywordCase,
  LogLevel,
  Theme,
  type AppInfo,
  type Config,
  type ConnectionDTO,
  type ServerInfo,
} from "@/services/backend";

export function testConnection(overrides: Partial<ConnectionDTO["profile"]> = {}, extra: Partial<ConnectionDTO> = {}): ConnectionDTO {
  return {
    profile: {
      id: "c1",
      name: "Sales reporting",
      server: "db1.corp.local",
      port: 0,
      database: "Sales",
      auth: AuthMethod.AuthSQL,
      username: "reader",
      encrypt: EncryptMode.EncryptMandatory,
      trustServerCertificate: false,
      connectTimeoutSeconds: 0,
      readOnlyIntent: false,
      createdAt: "2026-11-06T09:00:00Z",
      updatedAt: "2026-11-06T09:00:00Z",
      ...overrides,
    },
    hasPassword: true,
    connected: false,
    ...extra,
  };
}

export function testServer(): ServerInfo {
  return {
    serverName: "DB1",
    productVersion: "16.0.4305.5",
    productLevel: "RTM",
    edition: "Developer Edition (64-bit)",
    engineEdition: 3,
    cloud: false,
    database: "Sales",
    login: "reader",
  };
}

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
      lockTimeoutSeconds: 5,
    },
    formatting: { keywordCase: KeywordCase.KeywordUpper, indentSize: 4, useTabs: false },
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
