/**
 * Compile/import smoke for generated OpenAPI types.
 * Ensures CI typecheck exercises the generated wire surface.
 */
import { describe, expect, it } from "vitest";
import type { paths, components } from "./highland-v1";

describe("generated highland-v1 wire types", () => {
  it("exposes core public paths", () => {
    type Login = paths["/auth/login"]["post"];
    type Users = paths["/api/v1/users"]["get"];
    type Audit = paths["/api/v1/audit"]["get"];
    type Healthz = paths["/healthz"]["get"];
    // Type-only usage — assert the types are importable.
    const labels: string[] = [
      "login" satisfies keyof { login: Login },
      "users" satisfies keyof { users: Users },
      "audit" satisfies keyof { audit: Audit },
      "healthz" satisfies keyof { healthz: Healthz },
    ];
    expect(labels).toHaveLength(4);
  });

  it("exposes shared components", () => {
    type Role = components["schemas"]["Role"];
    const role: Role = "admin";
    expect(role).toBe("admin");
  });
});
