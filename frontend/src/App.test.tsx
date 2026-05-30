import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { afterEach, test, vi } from "vitest";
import App from "./App";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("renders signed-out screen when /api/me returns 401", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 401 })));

  render(<App />);

  expect(await screen.findByRole("link", { name: /sign in/i })).toHaveAttribute(
    "href",
    "/api/auth/login",
  );
  expect(screen.getByAltText("Spark")).toBeInTheDocument();
});

test("renders authenticated shell for signed-in users", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({ id: "u1", username: "jan", role: "user", displayName: "Jan" }),
    ),
  );

  render(<App />);

  expect(await screen.findByRole("button", { name: /new chat/i })).toBeInTheDocument();
  expect(screen.getByText("Jan")).toBeInTheDocument();
});

test("renders admin entry for admin users", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({ id: "u1", username: "jan", role: "admin", displayName: "Jan" }),
    ),
  );

  render(<App />);

  expect(await screen.findByRole("button", { name: /admin/i })).toBeInTheDocument();
});
