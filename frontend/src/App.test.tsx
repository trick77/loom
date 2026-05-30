import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import App from "./App";

test("renders the spark brand and new chat action", () => {
  render(<App />);
  expect(screen.getByText("spark")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /new chat/i })).toBeInTheDocument();
});
