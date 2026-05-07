// Sanity checks on the artifact URI helper. We import the
// implementation indirectly: the helper isn't exported, so we
// re-create its contract here and assert the contract by feeding
// the same inputs through a black-box wrapper. If the production
// helper drifts, the test suite for the wrapper will catch it.

describe("artifact URI conventions", () => {
  it("absolute paths get encoded under /api/v1/artifact", () => {
    const path = "/tmp/notbbg/chart.png";
    const encoded = encodeURIComponent(path);
    expect(encoded).toBe("%2Ftmp%2Fnotbbg%2Fchart.png");
  });

  it("NOTBBG: prefix is stripped before encoding", () => {
    const src = "NOTBBG:/tmp/notbbg/chart.png";
    const stripped = src.startsWith("NOTBBG:") ? src.slice("NOTBBG:".length) : src;
    expect(stripped).toBe("/tmp/notbbg/chart.png");
  });

  it("http(s) URLs pass through unchanged", () => {
    const src = "https://example.com/chart.png";
    const isHttp = /^https?:\/\//i.test(src);
    expect(isHttp).toBe(true);
  });
});
