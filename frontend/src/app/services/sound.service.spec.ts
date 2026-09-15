import { SoundService } from "./sound.service";

describe("SoundService", () => {
  it("does not throw when AudioContext is unavailable", () => {
    Object.defineProperty(window, "AudioContext", {
      configurable: true,
      value: undefined,
    });

    expect(() => new SoundService().enable()).not.toThrow();
  });
});
