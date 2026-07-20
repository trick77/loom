import { afterEach, describe, expect, test, vi } from "vitest";
import {
  deleteDocument,
  indexDocument,
  listDocuments,
  unindexDocument,
  uploadDocument,
  uploadImageAttachment,
} from "./documents";
import { AuthExpiredError } from "./http";

afterEach(() => {
  vi.unstubAllGlobals();
});

const document = {
  id: "doc_1",
  filename: "notes.pdf",
  mimeType: "application/pdf",
  sizeBytes: 1024,
  indexed: false,
  createdAt: "2026-06-01T00:00:00Z",
};

const image = {
  id: "art_1",
  displayFilename: "screenshot.png",
  mimeType: "image/png",
  sizeBytes: 42,
  downloadUrl: "/api/artifacts/art_1/download",
};

function stubFetch(response: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function pdf() {
  return new File(["%PDF"], "notes.pdf", { type: "application/pdf" });
}

function png() {
  return new File(["png"], "screenshot.png", { type: "image/png" });
}

describe("uploadDocument", () => {
  test("posts the file as multipart form data", async () => {
    const fetchMock = stubFetch(Response.json(document));

    await expect(uploadDocument(pdf())).resolves.toEqual(document);

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/documents/upload");
    expect(init.method).toBe("POST");
    const form = init.body as FormData;
    expect(form.get("file")).toBeInstanceOf(File);
    expect(form.get("threadId")).toBeNull();
    expect(form.get("projectId")).toBeNull();
  });

  test("includes thread and project ids when provided", async () => {
    const fetchMock = stubFetch(Response.json(document));

    await uploadDocument(pdf(), { threadId: "t1", projectId: "p1" });

    const form = (fetchMock.mock.calls[0][1] as RequestInit).body as FormData;
    expect(form.get("threadId")).toBe("t1");
    expect(form.get("projectId")).toBe("p1");
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(uploadDocument(pdf())).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("maps 415 to an unsupported format error", async () => {
    stubFetch(new Response("", { status: 415 }));

    await expect(uploadDocument(pdf())).rejects.toThrow("Unsupported document format");
  });

  test("maps 409 to the attachment limit error", async () => {
    stubFetch(new Response("", { status: 409 }));

    await expect(uploadDocument(pdf())).rejects.toThrow("A thread can have up to 10 attached files.");
  });

  test("maps 413 to a file size error", async () => {
    stubFetch(new Response("", { status: 413 }));

    await expect(uploadDocument(pdf())).rejects.toThrow("Files must be 25 MB or smaller.");
  });

  test("throws a generic error on other failures", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(uploadDocument(pdf())).rejects.toThrow("failed to upload document");
  });
});

describe("uploadImageAttachment", () => {
  test("posts the image to the image upload endpoint", async () => {
    const fetchMock = stubFetch(Response.json(image));

    await expect(uploadImageAttachment(png())).resolves.toEqual(image);

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/artifacts/images/upload");
    expect(init.method).toBe("POST");
    expect((init.body as FormData).get("file")).toBeInstanceOf(File);
  });

  test("includes thread and project ids when provided", async () => {
    const fetchMock = stubFetch(Response.json(image));

    await uploadImageAttachment(png(), { threadId: "t1", projectId: "p1" });

    const form = (fetchMock.mock.calls[0][1] as RequestInit).body as FormData;
    expect(form.get("threadId")).toBe("t1");
    expect(form.get("projectId")).toBe("p1");
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(uploadImageAttachment(png())).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("maps 415 to an unsupported image format error", async () => {
    stubFetch(new Response("", { status: 415 }));

    await expect(uploadImageAttachment(png())).rejects.toThrow("Unsupported image format");
  });

  test("maps 413 to a file size error", async () => {
    stubFetch(new Response("", { status: 413 }));

    await expect(uploadImageAttachment(png())).rejects.toThrow("Files must be 25 MB or smaller.");
  });

  test("throws a generic error on other failures", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(uploadImageAttachment(png())).rejects.toThrow("failed to upload image");
  });
});

describe("listDocuments", () => {
  test("lists documents without a project filter", async () => {
    const fetchMock = stubFetch(Response.json({ items: [document] }));

    await expect(listDocuments()).resolves.toEqual([document]);
    expect(fetchMock).toHaveBeenCalledWith("/api/documents");
  });

  test("encodes the project id filter", async () => {
    const fetchMock = stubFetch(Response.json({ items: [] }));

    await listDocuments("p 1/x");

    expect(fetchMock).toHaveBeenCalledWith("/api/documents?projectId=p%201%2Fx");
  });

  test("returns an empty list when the body has no items", async () => {
    stubFetch(Response.json({}));

    await expect(listDocuments()).resolves.toEqual([]);
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(listDocuments()).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(listDocuments()).rejects.toThrow("failed to load documents");
  });
});

describe("indexDocument", () => {
  test("posts to the index endpoint and returns the document", async () => {
    const fetchMock = stubFetch(Response.json({ ...document, indexed: true }));

    await expect(indexDocument("doc 1")).resolves.toEqual({ ...document, indexed: true });
    expect(fetchMock).toHaveBeenCalledWith("/api/documents/doc%201/index", { method: "POST" });
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(indexDocument("doc_1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(indexDocument("doc_1")).rejects.toThrow("failed to index document");
  });
});

describe("unindexDocument", () => {
  test("posts to the unindex endpoint", async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(unindexDocument("doc_1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/api/documents/doc_1/unindex", { method: "POST" });
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(unindexDocument("doc_1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(unindexDocument("doc_1")).rejects.toThrow("failed to unindex document");
  });
});

describe("deleteDocument", () => {
  test("deletes the document", async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(deleteDocument("doc 1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith("/api/documents/doc%201", { method: "DELETE" });
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(deleteDocument("doc_1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 409 }));

    await expect(deleteDocument("doc_1")).rejects.toThrow("failed to delete document");
  });
});
