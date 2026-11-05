import { describe, expect, it } from "vitest";
import { testConfig } from "@/test/fixtures";
import { applyDraft, toDraft } from "./settings-form";

describe("settings form", () => {
  it("round-trips numeric values", () => {
    const cfg = testConfig();
    const { config, errors } = applyDraft(cfg, toDraft(cfg));
    expect(errors).toEqual({});
    expect(config).toEqual(cfg);
  });

  it("parses edited values without mutating the original", () => {
    const cfg = testConfig();
    const draft = { ...toDraft(cfg), "execution.maxRows": " 250 " };
    const { config, errors } = applyDraft(cfg, draft);
    expect(errors).toEqual({});
    expect(config.execution.maxRows).toBe(250);
    expect(cfg.execution.maxRows).toBe(10000);
  });

  it("rejects values that are not whole numbers", () => {
    const cfg = testConfig();
    const draft = { ...toDraft(cfg), "execution.timeoutSeconds": "1.5", "history.retentionDays": "abc" };
    const { errors } = applyDraft(cfg, draft);
    expect(Object.keys(errors).sort()).toEqual(["execution.timeoutSeconds", "history.retentionDays"]);
  });
});
