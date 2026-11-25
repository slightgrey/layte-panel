import { describe, expect, it } from "vitest";
import { parseCSV, toCSV } from "./csv";

describe("csv", () => {
  it("parses quotes, commas and line endings", () => {
    expect(parseCSV('﻿from,to,code\r\n/a,"/b?x=1,2",301\n"/c ""q""",/d,\n\n')).toEqual([
      ["from", "to", "code"],
      ["/a", "/b?x=1,2", "301"],
      ['/c "q"', "/d", ""],
    ]);
  });

  it("round-trips", () => {
    const rows = [
      ["from", "to", "code"],
      ["/a", "https://x.example/?a=1,b", 302],
    ];
    expect(parseCSV(toCSV(rows))).toEqual(rows.map((r) => r.map(String)));
  });
});
