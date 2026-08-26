export type Participant = {
  id: string;
  name: string;
  role: string;
  lead: boolean;
  submitted: boolean;
  vote?: number;
};
export type RoomState = {
  id: string;
  cards: number[];
  roles: string[];
  participants: Participant[];
  revealed: boolean;
  average: number;
  roleAverages: Record<string, number>;
};
export type ServerEvent = {
  type: string;
  state?: RoomState;
  self?: string;
  error?: string;
};
