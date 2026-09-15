import { RoomApiService, RoomNotFoundError } from "./room-api.service";

describe("RoomApiService", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("maps a missing room to RoomNotFoundError", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 404 })));

    await expect(new RoomApiService().get("room-id")).rejects.toBeInstanceOf(
      RoomNotFoundError,
    );
  });

  it("rejects non-JSON create failures without exposing internal details", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("bad request", { status: 400 })));

    await expect(new RoomApiService().create([1], ["Backend"])).rejects.toThrow(
      "bad request",
    );
  });
});
