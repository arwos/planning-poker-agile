import { Injectable } from "@angular/core";

@Injectable({ providedIn: "root" })
export class SoundService {
  private context?: AudioContext;

  join(): void {
    this.play([523.25, 659.25], 0.09);
  }
  leave(): void {
    this.play([392, 293.66], 0.12);
  }
  reveal(): void {
    this.play([783.99, 1046.5, 1318.51], 0.16);
  }

  private play(frequencies: number[], duration: number): void {
    const context = this.audioContext();
    if (!context) return;
    void context.resume();
    const start = context.currentTime;
    frequencies.forEach((frequency, index): void => {
      const oscillator = context.createOscillator();
      const gain = context.createGain();
      oscillator.type = "sine";
      oscillator.frequency.setValueAtTime(frequency, start + index * 0.035);
      gain.gain.setValueAtTime(0.0001, start + index * 0.035);
      gain.gain.exponentialRampToValueAtTime(
        0.07,
        start + index * 0.035 + 0.01,
      );
      gain.gain.exponentialRampToValueAtTime(
        0.0001,
        start + index * 0.035 + duration,
      );
      oscillator.connect(gain).connect(context.destination);
      oscillator.start(start + index * 0.035);
      oscillator.stop(start + index * 0.035 + duration);
    });
  }

  private audioContext(): AudioContext | undefined {
    if (typeof window === "undefined") return undefined;
    this.context ??= new AudioContext();
    return this.context;
  }
}
