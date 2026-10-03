// Wire protocol types matching pkg/protocol/protocol.go

export const SERVER_VERSION = "1.0.0";

export const MsgHello = "HELLO";
export const MsgUserList = "USER_LIST";
export const MsgConnectRequest = "CONNECT_REQUEST";
export const MsgConnectResponse = "CONNECT_RESPONSE";
export const MsgKeyExchange = "KEY_EXCHANGE";
export const MsgChat = "CHAT";
export const MsgFileChunk = "FILE_CHUNK";
export const MsgDisconnect = "DISCONNECT";
export const MsgError = "ERROR";
export const MsgPing = "PING";
export const MsgPong = "PONG";

export const ErrCodeTargetNotFound = "TARGET_NOT_FOUND";
export const ErrCodeTargetBusy = "TARGET_BUSY";
export const ErrCodeSelfConnect = "SELF_CONNECT";
export const ErrCodeInvalidPacket = "INVALID_PACKET";

export const SHORT_ID_CHARS = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
export const SHORT_ID_LENGTH = 6;
export const MAX_MESSAGE_SIZE = 64 * 1024;

export interface Packet {
  type: string;
  sender_id?: string;
  target_id?: string;
  payload?: unknown;
  timestamp: string;
}

export interface ClientAttachment {
  id: string;
  peerID: string;
  resuming?: boolean; // true when this connection is being replaced by a resume
}

export function newPacket(
  type: string,
  targetID: string,
  payload: unknown | null
): Packet {
  return {
    type,
    target_id: targetID || undefined,
    payload: payload ?? undefined,
    timestamp: new Date().toISOString(),
  };
}

export function encodePacket(pkt: Packet): string {
  return JSON.stringify(pkt);
}

export function decodePacket(raw: string): Packet {
  const pkt = JSON.parse(raw) as Packet;
  if (!pkt.type) {
    throw new Error("missing type");
  }
  return pkt;
}

export function generateShortID(): string {
  const chars = SHORT_ID_CHARS;
  const bytes = new Uint8Array(SHORT_ID_LENGTH);
  crypto.getRandomValues(bytes);
  let id = "";
  for (let i = 0; i < SHORT_ID_LENGTH; i++) {
    id += chars[bytes[i] % chars.length];
  }
  return id;
}

export function generateUniqueID(existing: Set<string>): string {
  for (let attempt = 0; attempt < 20; attempt++) {
    const id = generateShortID();
    if (!existing.has(id)) {
      return id;
    }
  }
  throw new Error("failed to generate unique ID");
}

export function isValidShortID(id: string): boolean {
  if (id.length !== SHORT_ID_LENGTH) {
    return false;
  }
  for (const char of id) {
    if (!SHORT_ID_CHARS.includes(char)) {
      return false;
    }
  }
  return true;
}
