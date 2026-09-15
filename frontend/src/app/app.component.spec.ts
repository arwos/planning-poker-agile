import { TestBed } from "@angular/core/testing";
import { AppComponent } from "./app.component";

describe("AppComponent server event parsing", () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [AppComponent] }).compileComponents();
  });

  it("rejects malformed events instead of throwing in the socket callback", () => {
    const component = TestBed.createComponent(AppComponent).componentInstance;
    const parse = (component as unknown as {
      parseServerEvent(value: unknown): unknown;
    }).parseServerEvent.bind(component);

    expect(parse("not an object")).toBeUndefined();
    expect(parse({ type: "room_state", state: { id: 1 } })).toBeUndefined();
  });

  it("accepts a valid room event with a reconnect token", () => {
    const component = TestBed.createComponent(AppComponent).componentInstance;
    const parse = (component as unknown as {
      parseServerEvent(value: unknown): unknown;
    }).parseServerEvent.bind(component);

    expect(
      parse({
        type: "room_state",
        self: "participant",
        reconnect_token: "token",
        state: {
          id: "room",
          cards: [1],
          roles: ["Backend"],
          participants: [],
          revealed: false,
          average: 0,
          hasVotes: false,
          roleAverages: {},
        },
      }),
    ).toBeTruthy();
  });
});
