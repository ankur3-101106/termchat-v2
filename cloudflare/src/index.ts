import {
  ClientAttachment,
  ErrCodeInvalidPacket,
  ErrCodeSelfConnect,
  ErrCodeTargetBusy,
  ErrCodeTargetNotFound,
  MAX_MESSAGE_SIZE,
  MsgChat,
  MsgConnectRequest,
  MsgConnectResponse,
  MsgDisconnect,
  MsgError,
  MsgFileChunk,
  MsgHello,
  MsgKeyExchange,
  MsgPing,
  MsgPong,
  MsgUserList,
  Packet,
  SERVER_VERSION,
  decodePacket,
  encodePacket,
  generateUniqueID,
  generateShortID,
  isValidShortID,
  newPacket,
  SHORT_ID_CHARS,
  SHORT_ID_LENGTH,
} from "./protocol";

export interface Env {
  RELAY: DurableObjectNamespace;
  SERVER_VERSION?: string;
}

/** Durable Object that mirrors the Go relay server logic. */
export class RelayServer implements DurableObject {
  constructor(
    private readonly state: DurableObjectState,
    private readonly env: Env
  ) {}

  async fetch(request: Request): Promise<Response> {
    if (request.headers.get("Upgrade") !== "websocket") {
      return new Response("Expected WebSocket", { status: 426 });
    }

    const pair = new WebSocketPair();
    const [client, server] = Object.values(pair);

    // Accept the client-provided ID from either query param name.
    const url = new URL(request.url);
    const requestedID = url.searchParams.get("id") || url.searchParams.get("client_id") || "";

    const existing = this.collectIDs();
    let id: string;
    let isResume = false;

    if (requestedID && isValidShortID(requestedID)) {
      id = requestedID;
      if (existing.has(id)) {
        isResume = true;
        // Mark the old connection as resuming so its webSocketClose doesn't notify the peer.
        // Also preserve the peerID for the new connection.
        const oldWS = this.findSocket(id);
        let preservedPeerID = "";
        if (oldWS) {
          const oldAtt = oldWS.deserializeAttachment() as ClientAttachment;
          preservedPeerID = oldAtt.peerID;
          oldAtt.resuming = true;
          oldWS.serializeAttachment(oldAtt);
          oldWS.close(1000, "resumed by new connection");
        }
        this.state.acceptWebSocket(server);
        server.serializeAttachment({ id, peerID: preservedPeerID } satisfies ClientAttachment);
      }
    } else {
      id = generateUniqueID(existing);
      this.state.acceptWebSocket(server);
      server.serializeAttachment({ id, peerID: "" } satisfies ClientAttachment);
    }

    const version = this.env.SERVER_VERSION || SERVER_VERSION;
    const hello = newPacket(MsgHello, "", {
      assigned_id: id,
      server_version: version,
    });
    server.send(encodePacket(hello));

    if (isResume) {
      console.log(`[relay] session resumed for client ${id}`);
    } else {
      console.log(`[relay] new session created for client ${id}`);
    }

    this.broadcastUserList();

    return new Response(null, { status: 101, webSocket: client });
  }

  async webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void> {
    const raw =
      typeof message === "string" ? message : new TextDecoder().decode(message);

    if (raw.length > MAX_MESSAGE_SIZE) {
      this.sendError(ws, ErrCodeInvalidPacket, "message too large");
      return;
    }

    let pkt: Packet;
    try {
      pkt = decodePacket(raw);
    } catch (err) {
      this.sendError(
        ws,
        ErrCodeInvalidPacket,
        "malformed packet: " + (err instanceof Error ? err.message : String(err))
      );
      return;
    }

    const attachment = ws.deserializeAttachment() as ClientAttachment;
    pkt.sender_id = attachment.id;

    switch (pkt.type) {
      case MsgPing:
        this.handlePing(ws);
        break;
      case MsgConnectRequest:
        this.handleConnectRequest(ws, attachment, pkt);
        break;
      case MsgConnectResponse:
        this.handleConnectResponse(ws, attachment, pkt);
        break;
      case MsgKeyExchange:
      case MsgChat:
      case MsgFileChunk:
      case MsgDisconnect:
        this.forwardToTarget(ws, attachment, pkt);
        break;
      default:
        this.sendError(ws, ErrCodeInvalidPacket, `unknown message type: ${pkt.type}`);
    }
  }

  async webSocketClose(
    ws: WebSocket,
    _code: number,
    _reason: string,
    _wasClean: boolean
  ): Promise<void> {
    const attachment = ws.deserializeAttachment() as ClientAttachment | null;
    if (!attachment) {
      return;
    }

    // Skip peer disconnect notification if this connection is being resumed.
    if (!attachment.resuming && attachment.peerID) {
      this.notifyPeerDisconnected(attachment.peerID, attachment.id);
    }

    this.broadcastUserList();
  }

  private collectIDs(): Set<string> {
    const ids = new Set<string>();
    for (const ws of this.state.getWebSockets()) {
      const att = ws.deserializeAttachment() as ClientAttachment | null;
      if (att?.id) {
        ids.add(att.id);
      }
    }
    return ids;
  }

  private findSocket(clientID: string): WebSocket | undefined {
    for (const ws of this.state.getWebSockets()) {
      const att = ws.deserializeAttachment() as ClientAttachment | null;
      if (att?.id === clientID) {
        return ws;
      }
    }
    return undefined;
  }

  private broadcastUserList(): void {
    const sockets = this.state.getWebSockets();
    const users: string[] = [];
    for (const ws of sockets) {
      const att = ws.deserializeAttachment() as ClientAttachment | null;
      if (att?.id) {
        users.push(att.id);
      }
    }

    const pkt = newPacket(MsgUserList, "", { users });
    const data = encodePacket(pkt);
    for (const ws of sockets) {
      try {
        ws.send(data);
      } catch {
        // Drop if socket is closing.
      }
    }
  }

  private notifyPeerDisconnected(recipientID: string, disconnectedID: string): void {
    const recipient = this.findSocket(recipientID);
    if (!recipient) {
      return;
    }

    const att = recipient.deserializeAttachment() as ClientAttachment;
    att.peerID = "";
    recipient.serializeAttachment(att);

    const pkt = newPacket(MsgDisconnect, "", {
      reason: `peer ${disconnectedID} disconnected`,
    });
    pkt.sender_id = disconnectedID;
    try {
      recipient.send(encodePacket(pkt));
    } catch {
      // Ignore send failures on disconnect.
    }
  }

  private handlePing(ws: WebSocket): void {
    const pkt = newPacket(MsgPong, "", null);
    try {
      ws.send(encodePacket(pkt));
    } catch {
      // Ignore.
    }
  }

  private handleConnectRequest(
    senderWS: WebSocket,
    sender: ClientAttachment,
    pkt: Packet
  ): void {
    const targetID = pkt.target_id || "";
    if (!targetID) {
      this.sendError(senderWS, ErrCodeInvalidPacket, "connect request missing target_id");
      return;
    }
    if (targetID === sender.id) {
      this.sendError(senderWS, ErrCodeSelfConnect, "cannot connect to yourself");
      return;
    }

    const targetWS = this.findSocket(targetID);
    if (!targetWS) {
      this.sendError(
        senderWS,
        ErrCodeTargetNotFound,
        `user ${targetID} is not connected`
      );
      return;
    }

    const target = targetWS.deserializeAttachment() as ClientAttachment;
    if (target.peerID) {
      this.sendError(
        senderWS,
        ErrCodeTargetBusy,
        `user ${targetID} is already in a session`
      );
      return;
    }

    try {
      targetWS.send(encodePacket(pkt));
    } catch {
      // Drop if buffer full / closing.
    }
  }

  private handleConnectResponse(
    senderWS: WebSocket,
    sender: ClientAttachment,
    pkt: Packet
  ): void {
    const targetID = pkt.target_id || "";
    if (!targetID) {
      this.sendError(senderWS, ErrCodeInvalidPacket, "connect response missing target_id");
      return;
    }

    const payload = pkt.payload as { accepted?: boolean } | undefined;
    const accepted = payload?.accepted === true;

    const targetWS = this.findSocket(targetID);
    if (!targetWS) {
      return;
    }

    if (accepted) {
      sender.peerID = targetID;
      senderWS.serializeAttachment(sender);

      const target = targetWS.deserializeAttachment() as ClientAttachment;
      target.peerID = sender.id;
      targetWS.serializeAttachment(target);
    }

    try {
      targetWS.send(encodePacket(pkt));
    } catch {
      // Drop if buffer full / closing.
    }
  }

  private forwardToTarget(
    senderWS: WebSocket,
    sender: ClientAttachment,
    pkt: Packet
  ): void {
    const targetID = pkt.target_id || "";
    if (!targetID) {
      this.sendError(senderWS, ErrCodeInvalidPacket, "packet missing target_id");
      return;
    }

    const targetWS = this.findSocket(targetID);
    if (!targetWS) {
      this.sendError(
        senderWS,
        ErrCodeTargetNotFound,
        `user ${targetID} is not connected`
      );
      return;
    }

    if (pkt.type === MsgDisconnect) {
      sender.peerID = "";
      senderWS.serializeAttachment(sender);

      const target = targetWS.deserializeAttachment() as ClientAttachment;
      target.peerID = "";
      targetWS.serializeAttachment(target);
    }

    try {
      targetWS.send(encodePacket(pkt));
    } catch {
      // Drop if buffer full / closing.
    }
  }

  private sendError(ws: WebSocket, code: string, message: string): void {
    const pkt = newPacket(MsgError, "", { code, message });
    try {
      ws.send(encodePacket(pkt));
    } catch {
      // Ignore.
    }
  }
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (url.pathname === "/health") {
      return Response.json({
        status: "ok",
        version: env.SERVER_VERSION || SERVER_VERSION,
      });
    }

    if (url.pathname === "/") {
      return new Response("TermChat relay server is running.\n", {
        headers: { "Content-Type": "text/plain" },
      });
    }

    if (url.pathname === "/ws") {
      const id = env.RELAY.idFromName("global");
      const stub = env.RELAY.get(id);
      return stub.fetch(request);
    }

    return new Response("Not Found", { status: 404 });
  },
};
