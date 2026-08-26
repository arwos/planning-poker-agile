import { CommonModule } from "@angular/common";
import { Component, inject, signal } from "@angular/core";
import { FormsModule } from "@angular/forms";
import { Participant, RoomState, ServerEvent } from "./models/room";
import { RoomApiService } from "./services/room-api.service";
import { SoundService } from "./services/sound.service";

type RoomSettings = { cards: number[]; roles: string[] };
const defaultCards = [0, 0.5, 1, 2, 3, 5, 8];
const defaultRoles = ["Backend", "Frontend", "QA", "Analytic"];
const roomSettingsKey = "planning-poker.room-settings";
const lastRoleKey = "planning-poker.last-role";

@Component({
  selector: "app-root",
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: "./app.component.html",
  styleUrl: "./app.component.css",
})
export class AppComponent {
  private readonly api = inject(RoomApiService);
  private readonly sounds = inject(SoundService);
  readonly mode = signal<"create" | "join" | "game">("create");
  readonly room = signal<RoomState | null>(null);
  readonly error = signal("");
  readonly link = signal("");
  readonly copyStatus = signal<"idle" | "success" | "error">("idle");
  cards = [...defaultCards];
  roles = [...defaultRoles];
  newCard = "";
  newRole = "";
  name = localStorage.getItem("poker-name") ?? "";
  role = "";
  selected?: number;
  selfId = "";
  socket?: WebSocket;
  roomId = "";
  reconnectAttempts = 0;
  leaving = false;
  constructor() {
    this.restoreSettings();
    const match = location.pathname.match(/^\/room\/([\w-]+)$/);
    if (match) {
      this.roomId = match[1];
      void this.loadRoom();
    }
  }
  addCard(): void {
    const value = Number(this.newCard.trim());
    if (
      this.newCard.trim() === "" ||
      !Number.isFinite(value) ||
      value < 0 ||
      this.cards.includes(value)
    ) {
      this.error.set("Enter a unique non-negative number for a story point.");
      return;
    }
    this.cards = [...this.cards, value].sort((left, right) => left - right);
    this.newCard = "";
    this.error.set("");
    this.saveSettings();
  }
  removeCard(index: number): void {
    this.cards.splice(index, 1);
    this.saveSettings();
  }
  addRole(): void {
    const value = this.newRole.trim();
    if (
      !value ||
      this.roles.some((role) => role.toLowerCase() === value.toLowerCase())
    ) {
      this.error.set("Enter a unique role name.");
      return;
    }
    this.roles.push(value);
    this.newRole = "";
    this.error.set("");
    this.saveSettings();
  }
  removeRole(index: number): void {
    this.roles.splice(index, 1);
    this.saveSettings();
  }
  async create(): Promise<void> {
    try {
      this.error.set("");
      if (this.cards.length === 0 || this.roles.length === 0)
        throw new Error("Add at least one story point and one role.");
      this.saveSettings();
      const data = await this.api.create(this.cards, this.roles);
      this.link.set(`${location.origin}${data.url}`);
    } catch (error) {
      this.error.set(
        error instanceof Error ? error.message : "Could not create room.",
      );
    }
  }
  async loadRoom(): Promise<void> {
    try {
      const room = await this.api.get(this.roomId);
      this.room.set(room);
      const savedRole = localStorage.getItem(lastRoleKey) ?? "";
      this.role = room.roles.includes(savedRole) ? savedRole : "";
      this.mode.set("join");
    } catch (error) {
      this.error.set(
        error instanceof Error ? error.message : "Could not load room.",
      );
      this.mode.set("join");
    }
  }
  join(): void {
    this.leaving = false;
    localStorage.setItem("poker-name", this.name.trim());
    this.socket = new WebSocket(this.api.webSocketURL(this.roomId));
    this.socket.onopen = (): void => {
      this.reconnectAttempts = 0;
      this.error.set("");
      this.send("join", { name: this.name.trim(), role: this.role });
    };
    this.socket.onmessage = (event: MessageEvent): void =>
      this.handleEvent(JSON.parse(event.data) as ServerEvent);
    this.socket.onerror = (): void =>
      this.error.set("Connection to the room failed.");
    this.socket.onclose = (): void => this.reconnect();
  }
  private handleEvent(event: ServerEvent): void {
    if (event.type === "error") {
      this.error.set(event.error || "Could not join the room.");
      return;
    }
    if (event.type === "participant_joined") this.sounds.join();
    if (event.type === "participant_left") this.sounds.leave();
    if (event.type === "results_revealed") this.sounds.reveal();
    if (event.type === "voting_reset") this.selected = undefined;
    if (event.state) {
      this.room.set(event.state);
      if (event.self) this.selfId = event.self;
      this.mode.set("game");
    }
  }
  private reconnect(): void {
    if (!this.leaving && this.reconnectAttempts < 3) {
      this.reconnectAttempts += 1;
      this.error.set("Reconnecting to the room…");
      window.setTimeout((): void => this.join(), this.reconnectAttempts * 1000);
    } else if (!this.leaving)
      this.error.set("Connection closed. Please reload to try again.");
  }
  me(): Participant | undefined {
    return this.room()?.participants.find(
      (item): boolean => item.id === this.selfId,
    );
  }
  participantAngle(index: number, count: number): string {
    return `${(360 / count) * index - 90}deg`;
  }
  roleColor(role: string): string {
    const index = this.room()?.roles.indexOf(role) ?? -1;
    if (index < 0) return "#8797ba";
    const hue = ((index % 50) * 137.508) % 360;
    return `hsl(${hue} 88% 64%)`;
  }
  roleParticipantCount(role: string): number {
    return (
      this.room()?.participants.filter(
        (participant) => participant.role === role,
      ).length ?? 0
    );
  }
  selectRole(role: string): void {
    this.role = role;
    localStorage.setItem(lastRoleKey, role);
  }
  vote(): void {
    if (this.selected !== undefined)
      this.send("vote_submitted", { value: this.selected });
  }
  reset(): void {
    this.send("reset", {});
    this.selected = undefined;
  }
  send(type: string, payload: object): void {
    this.socket?.send(JSON.stringify({ type, ...payload }));
  }
  leave(): void {
    this.leaving = true;
    this.socket?.close();
    location.assign("/");
  }
  async copy(value: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      this.copyStatus.set("success");
    } catch {
      this.copyStatus.set("error");
    }
    window.setTimeout((): void => this.copyStatus.set("idle"), 1800);
  }
  private saveSettings(): void {
    localStorage.setItem(
      roomSettingsKey,
      JSON.stringify({
        cards: this.cards,
        roles: this.roles,
      } satisfies RoomSettings),
    );
  }
  private restoreSettings(): void {
    try {
      const saved = JSON.parse(
        localStorage.getItem(roomSettingsKey) ?? "null",
      ) as RoomSettings | null;
      if (!saved) return;
      if (
        Array.isArray(saved.cards) &&
        saved.cards.every((card) => Number.isFinite(card) && card >= 0)
      )
        this.cards = [...new Set(saved.cards)].sort(
          (left, right) => left - right,
        );
      if (
        Array.isArray(saved.roles) &&
        saved.roles.every((role) => typeof role === "string" && role.trim())
      )
        this.roles = saved.roles.map((role) => role.trim());
    } catch {
      localStorage.removeItem(roomSettingsKey);
    }
  }
}
