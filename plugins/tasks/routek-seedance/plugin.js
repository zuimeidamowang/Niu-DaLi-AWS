// Current channel contract: https://routek.ai/docs/doubao-seedance.md.
// Legacy full-resolution profiles: https://routek.ai/docs/doubao-seedance-full.md,
// https://routek.ai/docs/sd25-ch4.md and https://routek.ai/docs/sd20-933.md.
// Retail prices belong to the administrator; the upstream may bill differently.
const MODELS = {
  "doubao-seedance-2-0-260128": { kind: "ark", resolutions: ["480p", "720p", "1080p"] },
  "doubao-seedance-2-0-fast-260128": { kind: "ark", resolutions: ["480p", "720p"] },
  "doubao-seedance-2-0-mini-260615": { kind: "ark", resolutions: ["480p", "720p"] },
  "doubao-seedance-2-5-260628": { kind: "ark", resolutions: ["480p", "720p", "1080p"] },
  // Public names keep their client identity; channel model_mapping selects the dated upstream model.
  "doubao-seedance-2-0": { kind: "ark", resolutions: ["480p", "720p", "1080p"] },
  "doubao-seedance-2-0-fast": { kind: "ark", resolutions: ["480p", "720p"] },
  "doubao-seedance-2-0-mini": { kind: "ark", resolutions: ["480p", "720p"] },
  "doubao-seedance-2-5": { kind: "ark", resolutions: ["480p", "720p", "1080p"] },
  "sd-2.5-ch4": { kind: "sd25", resolutions: ["720p"], min: 4, max: 30, seconds: 4 },
  "SD2.0满血稳定933": { kind: "sd20", resolutions: ["720p"], min: 5, max: 15, seconds: 15 },
};
const SECONDS = { type: "number", unit: "second", description: { en: "Video generation unit price", zh: "视频生成单价" } };
const BILLABLE_SECONDS = { type: "number", unit: "second", aggregate: true, description: { en: "Standard video billing unit price", zh: "标准视频计费单价" } };
const RESOLUTION = { en: "Output video resolution", zh: "输出视频分辨率" };
const VIDEO_INPUT = {
  enum: ["none", "video"],
  description: { en: "Reference video processing", zh: "参考视频处理" },
  enumLabels: {
    none: { en: "Without reference video processing", zh: "不含参考视频处理" },
    video: { en: "With reference video processing", zh: "含参考视频处理" },
  },
};
// Fixed tier baselines define standard billing seconds independently of the
// delivered aspect ratio. Multiplying the same baseline into the configured
// per-second price preserves the upstream token cost without rounding usage.
const TOKENS_PER_SECOND = { "480p": 854 * 480 * 24 / 1024, "720p": 21600, "1080p": 48600 };
const ARK_USAGE_FIELDS = { seconds: SECONDS, billable_seconds: BILLABLE_SECONDS, video_input: VIDEO_INPUT };
const ARK_USAGE_EXAMPLES = [
  { label: "720p · 6s", facts: { seconds: 6, billable_seconds: 6, resolution: "720p", video_input: "none" } },
  // Standard billing seconds include all processing, not a physical input duration.
  { label: "720p · 10 standard seconds · Video reference", facts: { seconds: 6, billable_seconds: 10, resolution: "720p", video_input: "video" } },
];
const RATIOS = ["21:9", "16:9", "3:2", "4:3", "1:1", "3:4", "2:3", "9:16"];

export const meta = {
  apiVersion: 1,
  key: "routek-seedance",
  name: "Routek Seedance",
  icon: "text:RK",
  version: "1.2.0",
  author: { name: "Niu Dali" },
  description: { en: "Routek Seedance video generation billed in standard seconds including reference processing", zh: "Routek Seedance 视频生成与含参考处理的标准秒计费" },
  baseUrl: "https://routek.ai",
  auth: "api_key",
  models: Object.keys(MODELS),
  fetchMode: "per_task",
  protocols: ["openai_video"],
  requiredCapabilities: ["media-assets@1"],
  routes: [
    { method: "POST", path: "/routek/api/v3/contents/generations/tasks", type: "submit", decode: "createTask", render: "taskCreated", models: Object.keys(MODELS).filter(model => MODELS[model].kind === "ark") },
    { method: "GET", path: "/routek/api/v3/contents/generations/tasks/:task_id", type: "query", render: "taskStatus" },
  ],
  usageSchema: { ...ARK_USAGE_FIELDS, resolution: { enum: ["480p", "720p", "1080p"], description: RESOLUTION } },
  usageExamples: ARK_USAGE_EXAMPLES,
  usageProfiles: Object.entries(MODELS).map(([model, config]) => ({
    models: [model],
    schema: { ...(config.kind === "ark" ? ARK_USAGE_FIELDS : { seconds: SECONDS }), resolution: { enum: config.resolutions, description: RESOLUTION } },
    examples: config.kind === "ark" ? ARK_USAGE_EXAMPLES : [{ label: "720p · 6s", facts: { seconds: 6, resolution: "720p" } }],
  })),
};

function modelConfig(model) {
  if (!Object.prototype.hasOwnProperty.call(MODELS, model)) throw new Error("Unsupported Routek Seedance model");
  return MODELS[model];
}

function exclusiveValue(values, name, fallback) {
  const present = values.filter(value => value !== undefined);
  if (present.length > 1) throw new Error("Provide only one " + name + " parameter");
  return present.length ? present[0] : fallback;
}

function mediaURL(value, assets) {
  return typeof value === "string" && (/^https:\/\/[^\s/@?#\\]+(?:[/?#][^\s\\]*)?$/.test(value) || assets && /^asset:\/\/ast-[a-zA-Z0-9_-]+$/.test(value));
}

function mediaList(value, name, maximum, assets) {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.length > maximum || value.some(url => !mediaURL(url, assets))) throw new Error(name + " must contain at most " + maximum + " valid media URLs");
  return value.slice();
}

function durationValue(value, config) {
  // 3600 is the host's MaxTaskDurationSeconds ceiling. The Ark document leaves
  // narrower model limits to the provider; SD documents specify their limits.
  if (!Number.isSafeInteger(value) || value < (config.min || 1) || value > (config.max || 3600)) throw new Error("Video duration must be an integer between " + (config.min || 1) + " and " + (config.max || 3600));
  return value;
}

// The public contract uses platform asset IDs. Only the host resolves them to
// request-scoped transport URLs; client-supplied URLs in ctx.body are untrusted.
function resolveMedia(req, assets) {
  if (!req || !Object.prototype.hasOwnProperty.call(req, "media")) return req;
  if (!Array.isArray(req.media) || !req.media.length || req.media.length > 32) throw new Error("media must contain between 1 and 32 assets");
  const legacy = ["images", "image_refs", "video_refs", "audio_refs", "reference_audios", "first_image", "last_image", "first_frame_image", "last_frame_image", "content"];
  if (legacy.some(key => req[key] !== undefined) || req.metadata && ["content", "referenceVideos", "referenceAudios", "firstFrame", "lastFrame"].some(key => req.metadata[key] !== undefined)) throw new Error("media cannot be combined with legacy media parameters");
  const result = { ...req };
  delete result.media;
  const images = [], videos = [];
  for (const item of req.media) {
    if (!item || typeof item !== "object" || Array.isArray(item) || Object.keys(item).length !== 2 || typeof item.asset_id !== "string" || typeof item.role !== "string") throw new Error("Each media entry requires asset_id and role");
    const asset = assets && Object.prototype.hasOwnProperty.call(assets, item.asset_id) && assets[item.asset_id];
    if (!asset || !mediaURL(asset.url, false)) throw new Error("Media asset is unavailable");
    if (item.role === "reference_video") {
      if (asset.type !== "video") throw new Error("reference_video requires a video asset");
      videos.push(asset.url);
    } else {
      if (asset.type !== "image") throw new Error("Image roles require an image asset");
      if (item.role === "reference_image") images.push(asset.url);
      else if (item.role === "first_frame" || item.role === "last_frame") {
        const key = item.role === "first_frame" ? "first_image" : "last_image";
        if (result[key] !== undefined) throw new Error("Provide at most one " + item.role);
        result[key] = asset.url;
      } else throw new Error("Unsupported media role");
    }
  }
  if (images.length) result.images = images;
  if (videos.length) result.video_refs = videos;
  return result;
}

function normalizeRequest(req, model, assets) {
  req = resolveMedia(req, assets);
  const config = modelConfig(model);
  if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("Request must be an object");
  const allowed = ["model", "prompt", "seconds", "duration", "resolution", "ratio", "size", "aspect_ratio", "images", "image_refs", "video_refs", "audio_refs", "reference_audios", "first_image", "last_image", "first_frame_image", "last_frame_image", "content", "metadata", "seed", "generate_audio", "watermark"];
  for (const key of Object.keys(req)) {
    if (!allowed.includes(key)) throw new Error("Unsupported parameter: " + key);
  }
  const metadata = req.metadata === undefined ? {} : req.metadata;
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) throw new Error("metadata must be an object");
  for (const key of Object.keys(metadata)) {
    if (!["content", "referenceVideos", "referenceAudios", "firstFrame", "lastFrame", "resolution", "generate_audio", "seed", "watermark"].includes(key)) throw new Error("Unsupported metadata parameter: " + key);
  }
  let seconds = exclusiveValue([req.seconds, req.duration], "duration", config.seconds || 5);
  if (req.seconds !== undefined && typeof seconds === "string" && /^[0-9]+$/.test(seconds)) seconds = Number(seconds);
  durationValue(seconds, config);
  const resolution = exclusiveValue([req.resolution, metadata.resolution], "resolution", "720p");
  // This installed channel uses the 720p/1080p group. Keep the historical
  // pricing profiles and completion facts compatible with existing tasks.
  if (config.kind === "ark" && resolution === "480p") throw new Error("This Routek channel supports 720p or 1080p; 480p requires an upstream full-resolution group");
  if (!config.resolutions.includes(resolution)) throw new Error("Unsupported resolution for this model");
  const ratio = exclusiveValue([req.ratio, req.size, req.aspect_ratio], "aspect ratio", "16:9");
  if (!RATIOS.includes(ratio) || config.kind === "ark" && ["3:2", "2:3"].includes(ratio)) throw new Error("Unsupported aspect ratio");
  if (req.prompt !== undefined && typeof req.prompt !== "string") throw new Error("prompt must be a string");
  const prompt = (req.prompt || "").trim();
  const images = mediaList(exclusiveValue([req.images, req.image_refs], "reference images"), "images", config.kind === "sd25" ? 30 : 9, config.kind === "ark");
  const videos = mediaList(exclusiveValue([req.video_refs, metadata.referenceVideos], "reference videos"), "video_refs", config.kind === "sd25" ? 10 : config.kind === "sd20" ? 0 : 3, config.kind === "ark");
  const audios = mediaList(exclusiveValue([req.audio_refs, req.reference_audios, metadata.referenceAudios], "reference audios"), "audio_refs", config.kind === "sd25" ? 10 : 3, config.kind === "ark");
  const first = exclusiveValue([req.first_image, req.first_frame_image, metadata.firstFrame], "first frame");
  const last = exclusiveValue([req.last_image, req.last_frame_image, metadata.lastFrame], "last frame");
  if (first !== undefined && !mediaURL(first, config.kind === "ark") || last !== undefined && !mediaURL(last, config.kind === "ark")) throw new Error("Frame images must be valid media URLs");
  if (last !== undefined && first === undefined || config.kind === "sd25" && first !== undefined && last === undefined) throw new Error("This frame mode requires both first and last images");
  if ((first !== undefined || last !== undefined) && images.length + videos.length + audios.length) throw new Error("Frame mode cannot be mixed with reference arrays");
  if (config.kind === "sd20" && audios.length && !images.length) throw new Error("Audio references require reference images");
  if (images.length + videos.length + audios.length > (config.kind === "sd25" ? 50 : 12)) throw new Error("Too many reference media items");

  const body = { model, resolution };
  if (config.kind === "ark") {
    const content = exclusiveValue([req.content, metadata.content], "content", []);
    if (!Array.isArray(content) || content.length > 20) throw new Error("content must be an array with at most 20 entries");
    body.content = [];
    let imageCount = images.length + (first !== undefined ? 1 : 0) + (last !== undefined ? 1 : 0);
    let videoCount = videos.length;
    let audioCount = audios.length;
    let firstCount = first !== undefined ? 1 : 0;
    let lastCount = last !== undefined ? 1 : 0;
    for (const item of content) {
      if (!item || typeof item !== "object" || Array.isArray(item)) throw new Error("Invalid content entry");
      if (item.type === "text") {
        if (Object.keys(item).some(key => !["type", "text"].includes(key)) || typeof item.text !== "string") throw new Error("Invalid text content");
        body.content.push({ type: "text", text: item.text });
        continue;
      }
      const roles = { image_url: ["reference_image", "first_frame", "last_frame"], video_url: ["reference_video"], audio_url: ["reference_audio"] };
      if (!Object.prototype.hasOwnProperty.call(roles, item.type)) throw new Error("Unsupported content type; draft tasks are not supported");
      const media = item[item.type];
      if (Object.keys(item).some(key => !["type", item.type, "role"].includes(key)) || !media || typeof media !== "object" || Array.isArray(media) || Object.keys(media).some(key => key !== "url") || !mediaURL(media.url, true) || !roles[item.type].includes(item.role)) throw new Error("Media content requires an HTTPS or asset://ast-... URL and a matching role beside type");
      body.content.push({ type: item.type, [item.type]: { url: media.url }, role: item.role });
      if (item.type === "image_url") imageCount++;
      if (item.type === "video_url") videoCount++;
      if (item.type === "audio_url") audioCount++;
      if (item.role === "first_frame") firstCount++;
      if (item.role === "last_frame") lastCount++;
    }
    if (imageCount > 9 || videoCount > 3 || audioCount > 3 || imageCount + videoCount + audioCount > 12) throw new Error("Too many reference media items");
    if (firstCount > 1 || lastCount > 1 || lastCount && !firstCount || (firstCount || lastCount) && (imageCount > firstCount + lastCount || videoCount || audioCount)) throw new Error("Use at most one first_frame and one last_frame; last_frame requires first_frame, and frame mode cannot mix with other references");
    if (prompt) body.content.push({ type: "text", text: prompt });
    for (const [type, urls, role] of [["image_url", images, "reference_image"], ["video_url", videos, "reference_video"], ["audio_url", audios, "reference_audio"]]) {
      for (const url of urls) body.content.push({ type, [type]: { url }, role });
    }
    if (first !== undefined) body.content.push({ type: "image_url", image_url: { url: first }, role: "first_frame" });
    if (last !== undefined) body.content.push({ type: "image_url", image_url: { url: last }, role: "last_frame" });
    if (body.content.length > 20) throw new Error("content must be an array with at most 20 entries");
    if (!body.content.length || !body.content.some(item => item.type !== "text" || item.text.trim())) throw new Error("prompt or reference content is required");
    // Prompt flags can override body fields in Ark-compatible services. Require
    // billing parameters in validated structured fields to prevent underbilling.
    if (body.content.some(item => item.type === "text" && /(?:^|\s)--(?:dur(?:ation)?|frames|resolution|res)(?:\s|=|$)/i.test(item.text))) throw new Error("Set duration and resolution in structured fields, not prompt flags");
    body.duration = seconds;
    body.ratio = ratio;
  } else {
    if (req.content !== undefined || metadata.content !== undefined) throw new Error("content is supported only by Ark models");
    if (!prompt || config.kind === "sd25" && prompt.length >= 5000) throw new Error("A non-empty prompt within the model limit is required");
    body.prompt = prompt;
    body.aspect_ratio = ratio;
    body[config.kind === "sd20" ? "seconds" : "duration"] = seconds;
    if (images.length) body[config.kind === "sd20" ? "images" : "image_refs"] = images;
    if (videos.length) body.video_refs = videos;
    if (audios.length) body[config.kind === "sd20" ? "reference_audios" : "audio_refs"] = audios;
    if (first !== undefined) body[config.kind === "sd20" ? "first_frame_image" : "first_image"] = first;
    if (last !== undefined) body[config.kind === "sd20" ? "last_frame_image" : "last_image"] = last;
  }
  for (const name of ["seed", "generate_audio", "watermark"]) {
    const value = exclusiveValue([req[name], metadata[name]], name);
    if (value === undefined) continue;
    if (name !== "seed" && config.kind !== "ark") throw new Error(name + " is unsupported for this model");
    if (name === "seed" ? !Number.isSafeInteger(value) : typeof value !== "boolean") throw new Error("Invalid " + name);
    body[name] = value;
  }
  return body;
}

function decodeRequest(ctx) {
  if (!ctx.body || !["json", "multipart"].includes(ctx.body.kind)) throw new Error("JSON or multipart body required");
  if ((ctx.body.files || []).length) throw new Error("Upload files to /v1/media/assets, then pass media asset_id references");
  let req = ctx.body.value;
  if (ctx.body.kind === "multipart") {
    req = {};
    for (const key of Object.keys(ctx.body.fields || {})) {
      const values = ctx.body.fields[key];
      if (!Array.isArray(values) || values.length !== 1) throw new Error("Each parameter must be provided once: " + key);
      let value = values[0];
      if (["media", "metadata", "content", "images", "image_refs", "video_refs", "audio_refs", "reference_audios"].includes(key)) {
        try { value = JSON.parse(value); } catch { throw new Error(key + " must contain valid JSON"); }
      } else if (["duration", "seed"].includes(key)) {
        if (!/^-?[0-9]+$/.test(value)) throw new Error(key + " must be an integer");
        value = Number(value);
      } else if (["generate_audio", "watermark"].includes(key)) {
        if (!["true", "false"].includes(value)) throw new Error(key + " must be a boolean");
        value = value === "true";
      }
      req[key] = value;
    }
  }
  const model = ctx.model || req && req.model;
  const requestBody = normalizeRequest(req, ctx.upstreamModel || model, ctx.assets);
  const reference = requestBody.content ? requestBody.content.some(item => item.type !== "text") : ["images", "image_refs", "video_refs", "audio_refs", "reference_audios", "first_image", "first_frame_image"].some(key => requestBody[key]);
  return { kind: "submit", model, action: reference ? "image_to_video" : "text_to_video", requestBody };
}

function taskID(value) {
  if (typeof value !== "string" || !/^[a-zA-Z0-9_-]{1,256}$/.test(value)) throw new Error("Invalid upstream task id");
  return value;
}

function apiRequest(ctx, path) {
  const base = String(ctx.baseUrl || "").replace(/\/+$/, "");
  if (!/^https?:\/\/[^\s/@?#\\]+(?:\/[^\s?#\\]*)?$/.test(base)) throw new Error("Invalid channel Base URL");
  if (typeof ctx.apiKey !== "string" || !ctx.apiKey.trim() || /[\r\n]/.test(ctx.apiKey)) throw new Error("Channel API key is required");
  return { url: base + path, method: "GET", headers: { Authorization: "Bearer " + ctx.apiKey } };
}

function taskPath(model) {
  return modelConfig(model).kind === "ark" ? "/api/v3/contents/generations/tasks" : "/v1/videos";
}

export function buildSubmitRequest(ctx) {
  if ((ctx.files || []).length) throw new Error("Upload files to /v1/media/assets, then pass media asset_id references");
  const model = ctx.upstreamModel || ctx.model;
  const request = apiRequest(ctx, taskPath(model));
  request.method = "POST";
  request.headers["Content-Type"] = "application/json";
  request.body = normalizeRequest(ctx.requestBody, model, ctx.assets);
  return request;
}

export function parseSubmitResponse(ctx, response) {
  const body = response.body || {};
  const result = { taskId: taskID(body.id), taskData: body };
  const model = ctx.upstreamModel || ctx.model;
  if (modelConfig(model).kind === "ark") {
    const request = normalizeRequest(ctx.requestBody, model, ctx.assets);
    result.state = { billing_resolution: request.resolution };
  }
  const immediate = parseTaskResult({ ...ctx, taskId: result.taskId }, body);
  if (["SUCCESS", "FAILURE"].includes(immediate.status)) result.immediate = immediate;
  return result;
}

export function extractUsage(ctx) {
  const model = ctx.upstreamModel || ctx.model;
  const body = normalizeRequest(ctx.requestBody, model, ctx.assets);
  if (ctx.usagePurpose === "billing_ratios") return null;
  const seconds = body.duration === undefined ? body.seconds : body.duration;
  const facts = { seconds, resolution: body.resolution };
  if (modelConfig(model).kind === "ark") {
    // Reserve output seconds only. Input duration is not available as trusted
    // asset metadata; completion settles all processing from actual token use.
    facts.billable_seconds = seconds;
    facts.video_input = body.content.some(item => item.type === "video_url") ? "video" : "none";
  }
  return facts;
}

export function extractUsageOnComplete(ctx, result, body) {
  if (result.status !== "SUCCESS" || !body || typeof body !== "object" || Array.isArray(body)) return {};
  const config = modelConfig(ctx.upstreamModel || ctx.model);
  const facts = {};
  // Physical output duration stays separate from aggregate billing seconds,
  // preserving existing per-output-second expressions for in-flight tasks.
  const seconds = body.duration === undefined ? body.seconds : body.duration;
  if (Number.isSafeInteger(seconds) && seconds >= (config.min || 1) && seconds <= (config.max || 3600)) facts.seconds = seconds;
  if (config.resolutions.includes(body.resolution)) facts.resolution = body.resolution;
  if (config.kind === "ark") {
    const resolution = facts.resolution || ctx.state && ctx.state.billing_resolution;
    const usage = body.usage;
    if (config.resolutions.includes(resolution) && usage && typeof usage === "object" && !Array.isArray(usage)) {
      // Routek settles total_tokens. completion_tokens is a compatibility
      // fallback only when total_tokens is absent, never when it is invalid.
      const tokens = usage.total_tokens === undefined ? usage.completion_tokens : usage.total_tokens;
      if (typeof tokens === "number" && Number.isFinite(tokens) && Number.isInteger(tokens) && tokens >= 0) {
        facts.billable_seconds = tokens / TOKENS_PER_SECOND[resolution];
      }
    }
  }
  return facts;
}

export function buildQueryRequest(ctx) {
  return apiRequest(ctx, taskPath(ctx.upstreamModel || ctx.model) + "/" + taskID(ctx.taskId));
}

function resultURL(body) {
  if (!body || typeof body !== "object") return "";
  const candidates = [body.url, body.video_url, body.content && body.content.video_url];
  return candidates.find(url => mediaURL(url, false)) || "";
}

export function parseTaskResult(ctx, body) {
  if (!body || typeof body !== "object" || Array.isArray(body)) return { status: "UNKNOWN" };
  if (body.id !== undefined && body.id !== ctx.taskId) return { status: "UNKNOWN", reason: "Upstream task id mismatch" };
  const statuses = modelConfig(ctx.upstreamModel || ctx.model).kind === "ark"
    ? { queued: "QUEUED", running: "IN_PROGRESS", processing: "IN_PROGRESS", succeeded: "SUCCESS", completed: "SUCCESS", failed: "FAILURE", expired: "FAILURE", cancelled: "FAILURE" }
    : { queued: "QUEUED", in_progress: "IN_PROGRESS", completed: "SUCCESS", failed: "FAILURE" };
  const status = Object.prototype.hasOwnProperty.call(statuses, body.status) ? statuses[body.status] : "UNKNOWN";
  const result = { status };
  if (status === "UNKNOWN") result.reason = "Unrecognized upstream status";
  if (status === "FAILURE") result.reason = body.error && typeof body.error.message === "string" ? body.error.message : "Video generation failed";
  if (status === "SUCCESS") {
    result.progress = "100%";
    const url = resultURL(body);
    if (url) result.url = url;
  } else if (typeof body.progress === "number" && Number.isFinite(body.progress) && body.progress >= 0 && body.progress <= 100) result.progress = body.progress + "%";
  return result;
}

export function listArtifacts(task) {
  return task.status === "SUCCESS" ? [{ key: "video", type: "video" }] : [];
}

export function buildContentRequest(ctx) {
  if (ctx.artifactKey !== "video") throw new Error("artifact_not_found");
  const request = apiRequest(ctx, "/v1/videos/" + taskID(ctx.upstreamTaskId) + "/content");
  const url = resultURL(ctx.data);
  // Routek's authenticated content endpoint uses the upstream key. External
  // signed CDN URLs are fetched without channel credentials or rewriting.
  // Provider docs still return the former rolldek.com host. Fetch protected
  // content through the configured channel, never send its key to that alias.
  const protectedURL = /^https:\/\/(?:routek\.ai|rolldek\.com)\/v1\/videos\/[a-zA-Z0-9_-]+\/content(?:[?#].*)?$/.test(url);
  if (url && url.split(/[?#]/, 1)[0] !== request.url && !protectedURL) return { url, method: "GET", credentialless: true };
  return request;
}

function publicTaskData(task) {
  const data = task.data && typeof task.data === "object" && !Array.isArray(task.data) ? task.data : {};
  const result = { id: task.task_id, content: {} };
  // Provider snapshots stay private for artifact downloads. Publish only
  // generation metadata so new vendor URL fields cannot expose access tokens.
  for (const name of ["model", "status", "duration", "seconds", "resolution", "ratio", "generate_audio", "watermark", "seed", "created_at", "updated_at", "completed_at"]) {
    if (data[name] !== undefined) result[name] = data[name];
  }
  if (data.usage && typeof data.usage === "object" && !Array.isArray(data.usage)) {
    const usage = {};
    for (const name of ["completion_tokens", "total_tokens", "prompt_tokens", "input_tokens", "output_tokens"]) {
      if (typeof data.usage[name] === "number" && Number.isFinite(data.usage[name]) && data.usage[name] >= 0) usage[name] = data.usage[name];
    }
    if (Object.keys(usage).length) result.usage = usage;
  }
  if (task.status === "SUCCESS") result.content.video_url = "/v1/videos/" + encodeURIComponent(task.task_id) + "/content";
  if (task.status === "FAILURE") result.error = { code: "video_generation_failed", message: "Video generation failed" };
  return result;
}

export const native = {
  createTask: decodeRequest,
  taskCreated(_ctx, task) { return { id: task.task_id }; },
  taskStatus(_ctx, task) { return publicTaskData(task); },
  error(_ctx, error) { return { error: { code: error.code, message: error.message } }; },
};

export const protocols = {
  openai_video: {
    decodeRequest,
    render(_ctx, task) { return publicTaskData(task); },
  },
};
