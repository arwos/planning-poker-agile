import { Injectable } from "@angular/core";

@Injectable({ providedIn: "root" })
export class SoundService {
  private context?: AudioContext;

  enable(): void {
    const context = this.audioContext();
    if (!context) return;
    void context.resume().catch(() => undefined);
  }

  join(): void {
    this.play([523.25, 659.25], 0.09);
  }
  leave(): void {
    this.play([392, 293.66], 0.12);
  }
  reveal(): void {
    const context = this.audioContext();
    if (!context) return;
    void context.resume().catch(() => undefined);

    const start = context.currentTime + 0.01;
    const melody = [196, 261.63, 329.63, 392, 523.25];
    melody.forEach((frequency, index): void => {
      this.playTone(context, frequency, start + index * 0.14, 0.28, 0.045, "sawtooth");
      this.playTone(context, frequency * 2, start + index * 0.14, 0.22, 0.014, "triangle");
    });

    [261.63, 329.63, 392, 523.25].forEach((frequency): void => {
      this.playTone(context, frequency, start + 0.58, 0.7, 0.025, "triangle");
    });
  }

  private play(frequencies: number[], duration: number): void {
    const context = this.audioContext();
    if (!context) return;
    void context.resume().catch(() => undefined);
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

  private playTone(
    context: AudioContext,
    frequency: number,
    start: number,
    duration: number,
    volume: number,
    type: OscillatorType,
  ): void {
    const oscillator = context.createOscillator();
    const gain = context.createGain();
    oscillator.type = type;
    oscillator.frequency.setValueAtTime(frequency, start);
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.exponentialRampToValueAtTime(volume, start + 0.015);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + duration);
    oscillator.connect(gain).connect(context.destination);
    oscillator.start(start);
    oscillator.stop(start + duration);
  }

  private audioContext(): AudioContext | undefined {
    if (
      typeof window === "undefined" ||
      typeof window.AudioContext !== "function"
    )
      return undefined;
    try {
      this.context ??= new window.AudioContext();
    } catch {
      return undefined;
    }
    return this.context;
  }
}
