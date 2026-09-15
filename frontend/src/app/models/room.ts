export type Participant = {
  id: string;
  name: string;
  role: string;
  lead: boolean;
  submitted: boolean;
  skipped: boolean;
  vote?: number;
};
export type RoomState = {
  id: string;
  cards: number[];
  roles: string[];
  participants: Participant[];
  revealed: boolean;
  average: number;
  hasVotes: boolean;
  roleAverages: Record<string, number>;
};
export type ServerEvent = {
  type: string;
  state?: RoomState;
  self?: string;
  reconnect_token?: string;
  error?: string;
};
