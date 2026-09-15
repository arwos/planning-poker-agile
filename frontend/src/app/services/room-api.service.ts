import { Injectable } from "@angular/core";
import { RoomState } from "../models/room";

export type CreatedRoom = {
  id: string;
  url: string;
  owner_token: string;
};

const apiBase =
  location.port === "4200" ? "http://localhost:8080" : location.origin;

export class RoomNotFoundError extends Error {
  constructor() {
    super("This room is unavailable or has expired.");
    this.name = "RoomNotFoundError";
  }
}

@Injectable({ providedIn: "root" })
export class RoomApiService {
  async create(
    cards: number[],
    roles: string[],
  ): Promise<CreatedRoom> {
    const response = await fetch(`${apiBase}/api/rooms`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ cards, roles }),
    });
    if (!response.ok)
      throw new Error(
        (await response.text()) || "Check card values and roles.",
      );
    return response.json();
  }
  async get(id: string): Promise<RoomState> {
    const response = await fetch(`${apiBase}/api/rooms/${id}`);
    if (response.status === 404) throw new RoomNotFoundError();
    if (!response.ok) throw new Error("Could not load the room.");
    return response.json();
  }
  webSocketURL(id: string): string {
    return `${apiBase.replace(/^http/, "ws")}/ws/rooms/${id}`;
  }
}
