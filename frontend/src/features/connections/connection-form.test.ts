import { describe, expect, it } from "vitest";
import { AuthMethod, EncryptMode, ErrorCode, PasswordAction } from "@/services/backend";
import { testConnection } from "@/test/fixtures";
import { emptyForm, errorHint, formFromConnection, toInput } from "./connection-form";

describe("connection form", () => {
  it("never pre-fills a password when editing", () => {
    const form = formFromConnection(testConnection());
    expect(form.password).toBe("");
    expect(form.hasStoredPassword).toBe(true);
  });

  it("keeps the stored password when the field is left empty", () => {
    const { input, errors } = toInput(formFromConnection(testConnection()));
    expect(errors).toEqual({});
    expect(input.passwordAction).toBe(PasswordAction.PasswordKeep);
    expect(input.password).toBe("");
  });

  it("sets or clears the password", () => {
    const base = formFromConnection(testConnection());
    expect(toInput({ ...base, password: "new" }).input).toMatchObject({ passwordAction: PasswordAction.PasswordSet, password: "new" });
    expect(toInput({ ...base, clearPassword: true }).input.passwordAction).toBe(PasswordAction.PasswordClear);
  });

  it("never sends a username or password for Windows authentication", () => {
    const form = { ...emptyForm(), auth: AuthMethod.AuthWindows, username: "sa", password: "typed" };
    const { input } = toInput(form);
    expect(input.profile.username).toBe("");
    expect(input.passwordAction).toBe(PasswordAction.PasswordKeep);
    expect(input.password).toBe("");
  });

  it("parses optional numbers and reports bad ones", () => {
    expect(toInput({ ...emptyForm(), port: "", timeout: "" }).input.profile).toMatchObject({ port: 0, connectTimeoutSeconds: 0 });
    expect(toInput({ ...emptyForm(), port: " 14333 ", timeout: "30" }).input.profile).toMatchObject({ port: 14333, connectTimeoutSeconds: 30 });
    expect(Object.keys(toInput({ ...emptyForm(), port: "14a", timeout: "-1" }).errors).sort()).toEqual(["connectTimeoutSeconds", "port"]);
  });

  it("drops trust-server-certificate with strict encryption", () => {
    const form = { ...emptyForm(), encrypt: EncryptMode.EncryptStrict, trustServerCertificate: true };
    expect(toInput(form).input.profile.trustServerCertificate).toBe(false);
  });

  it("gives specific hints for common failures", () => {
    const form = { server: String.raw`localhost\SQLEXPRESS`, database: "Sales" };
    expect(errorHint({ code: ErrorCode.ErrCertificate, serverErrorNumber: 0, message: "" }, form)).toMatch(/Trust server certificate/);
    expect(errorHint({ code: ErrorCode.ErrLoginFailed, serverErrorNumber: 4060, message: "" }, form)).toMatch(/cannot open database "Sales"/);
    expect(errorHint({ code: ErrorCode.ErrTimeout, serverErrorNumber: 0, message: "" }, form)).toMatch(/SQL Server Browser/);
  });
});
