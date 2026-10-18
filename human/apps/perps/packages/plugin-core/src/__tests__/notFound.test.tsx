import React from "react";
import { render, screen } from "@testing-library/react";
import { NotFound } from "../notFound";

describe("NotFound", () => {
  it("renders position and Not found! message", () => {
    render(<NotFound position="Deposit.DepositForm" />);
    expect(screen.getByText("[Deposit.DepositForm]")).toBeTruthy();
    expect(screen.getByText("Not found!")).toBeTruthy();
  });
});
