export namespace desktop {
	
	export class AVCSearchRow {
	    tagId: string;
	    dts: string;
	    offset: string;
	    declaredFrameType?: number;
	    observedIDR: string;
	    declarationRelation: string;
	    reason: string;
	    span?: detailview.Span;
	
	    static createFrom(source: any = {}) {
	        return new AVCSearchRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.dts = source["dts"];
	        this.offset = source["offset"];
	        this.declaredFrameType = source["declaredFrameType"];
	        this.observedIDR = source["observedIDR"];
	        this.declarationRelation = source["declarationRelation"];
	        this.reason = source["reason"];
	        this.span = this.convertValues(source["span"], detailview.Span);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AVCSearchPage {
	    start: string;
	    next: string;
	    total: string;
	    scanned: number;
	    bytesRead: string;
	    exhausted: boolean;
	    scanComplete: boolean;
	    filter: string;
	    rows: AVCSearchRow[];
	
	    static createFrom(source: any = {}) {
	        return new AVCSearchPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.next = source["next"];
	        this.total = source["total"];
	        this.scanned = source["scanned"];
	        this.bytesRead = source["bytesRead"];
	        this.exhausted = source["exhausted"];
	        this.scanComplete = source["scanComplete"];
	        this.filter = source["filter"];
	        this.rows = this.convertValues(source["rows"], AVCSearchRow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AVCSearchRequest {
	    sessionId: string;
	    requestId: string;
	    start: string;
	    filter: string;
	
	    static createFrom(source: any = {}) {
	        return new AVCSearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.start = source["start"];
	        this.filter = source["filter"];
	    }
	}
	export class Fault {
	    code: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Fault(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class AVCSearchResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    state: string;
	    page?: AVCSearchPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new AVCSearchResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.state = source["state"];
	        this.page = this.convertValues(source["page"], AVCSearchPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class CodecSearchValue {
	    name: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new CodecSearchValue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.value = source["value"];
	    }
	}
	export class CodecSearchRow {
	    tagId: string;
	    dts: string;
	    offset: string;
	    frameIndex?: number;
	    nalIndex?: number;
	    messageIndex?: number;
	    kind: string;
	    status: string;
	    contextState: string;
	    complete: boolean;
	    span?: detailview.Span;
	    spsSpan?: detailview.Span;
	    evidenceSpan?: detailview.Span;
	    values: CodecSearchValue[];
	
	    static createFrom(source: any = {}) {
	        return new CodecSearchRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.dts = source["dts"];
	        this.offset = source["offset"];
	        this.frameIndex = source["frameIndex"];
	        this.nalIndex = source["nalIndex"];
	        this.messageIndex = source["messageIndex"];
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.contextState = source["contextState"];
	        this.complete = source["complete"];
	        this.span = this.convertValues(source["span"], detailview.Span);
	        this.spsSpan = this.convertValues(source["spsSpan"], detailview.Span);
	        this.evidenceSpan = this.convertValues(source["evidenceSpan"], detailview.Span);
	        this.values = this.convertValues(source["values"], CodecSearchValue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CodecSearchPosition {
	    tagId: string;
	    frameIndex: number;
	
	    static createFrom(source: any = {}) {
	        return new CodecSearchPosition(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.frameIndex = source["frameIndex"];
	    }
	}
	export class CodecSearchPage {
	    kind: string;
	    filter: string;
	    cursor: string;
	    nextCursor: string;
	    start: CodecSearchPosition;
	    next: CodecSearchPosition;
	    total: string;
	    scannedTags: number;
	    scannedFrames: number;
	    pending: number;
	    bytesRead: string;
	    exhausted: boolean;
	    scanComplete: boolean;
	    rows: CodecSearchRow[];
	
	    static createFrom(source: any = {}) {
	        return new CodecSearchPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.filter = source["filter"];
	        this.cursor = source["cursor"];
	        this.nextCursor = source["nextCursor"];
	        this.start = this.convertValues(source["start"], CodecSearchPosition);
	        this.next = this.convertValues(source["next"], CodecSearchPosition);
	        this.total = source["total"];
	        this.scannedTags = source["scannedTags"];
	        this.scannedFrames = source["scannedFrames"];
	        this.pending = source["pending"];
	        this.bytesRead = source["bytesRead"];
	        this.exhausted = source["exhausted"];
	        this.scanComplete = source["scanComplete"];
	        this.rows = this.convertValues(source["rows"], CodecSearchRow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class CodecSearchRequest {
	    sessionId: string;
	    requestId: string;
	    cursor: string;
	    kind: string;
	    filter: string;
	
	    static createFrom(source: any = {}) {
	        return new CodecSearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.cursor = source["cursor"];
	        this.kind = source["kind"];
	        this.filter = source["filter"];
	    }
	}
	export class CodecSearchResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    state: string;
	    page?: CodecSearchPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new CodecSearchResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.state = source["state"];
	        this.page = this.convertValues(source["page"], CodecSearchPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class DetailQuery {
	    sessionId: string;
	    requestId: string;
	
	    static createFrom(source: any = {}) {
	        return new DetailQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	    }
	}
	export class DetailRequest {
	    sessionId: string;
	    requestId: string;
	    tagId: string;
	    mp3FrameIndex?: number;
	    mp3PageStart?: number;
	    mp3Dependency?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DetailRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.tagId = source["tagId"];
	        this.mp3FrameIndex = source["mp3FrameIndex"];
	        this.mp3PageStart = source["mp3PageStart"];
	        this.mp3Dependency = source["mp3Dependency"];
	    }
	}
	export class DetailResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    tagId: string;
	    state: string;
	    detail?: detailview.Detail;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new DetailResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.tagId = source["tagId"];
	        this.state = source["state"];
	        this.detail = this.convertValues(source["detail"], detailview.Detail);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Diagnostic {
	    code: string;
	    severity: string;
	    offset: string;
	    bitOffset: number;
	    message: string;
	    expected?: string;
	    actual?: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.severity = source["severity"];
	        this.offset = source["offset"];
	        this.bitOffset = source["bitOffset"];
	        this.message = source["message"];
	        this.expected = source["expected"];
	        this.actual = source["actual"];
	    }
	}
	export class PageBounds {
	    start: string;
	    limit: number;
	    total: string;
	    next: string;
	
	    static createFrom(source: any = {}) {
	        return new PageBounds(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.limit = source["limit"];
	        this.total = source["total"];
	        this.next = source["next"];
	    }
	}
	export class DiagnosticPage {
	    sessionId: string;
	    bounds: PageBounds;
	    diagnostics: Diagnostic[];
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.bounds = this.convertValues(source["bounds"], PageBounds);
	        this.diagnostics = this.convertValues(source["diagnostics"], Diagnostic);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiagnosticPageResponse {
	    apiVersion: number;
	    page?: DiagnosticPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticPageResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.page = this.convertValues(source["page"], DiagnosticPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class PreviousTagSize {
	    present: boolean;
	    span?: Span;
	    value: number;
	    expected: number;
	    valid: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PreviousTagSize(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.present = source["present"];
	        this.span = this.convertValues(source["span"], Span);
	        this.value = source["value"];
	        this.expected = source["expected"];
	        this.valid = source["valid"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Span {
	    byteOffset: string;
	    bitOffset: number;
	    bitLength: number;
	
	    static createFrom(source: any = {}) {
	        return new Span(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.byteOffset = source["byteOffset"];
	        this.bitOffset = source["bitOffset"];
	        this.bitLength = source["bitLength"];
	    }
	}
	export class Tag {
	    id: string;
	    typeCode: number;
	    typeName: string;
	    offset: string;
	    span: Span;
	    headerSpan: Span;
	    payloadSpan: Span;
	    rawTypeFlags: number;
	    filter: boolean;
	    dataSize: number;
	    availableDataSize: number;
	    dts: string;
	    streamId: number;
	    payloadComplete: boolean;
	    previousTagSize: PreviousTagSize;
	
	    static createFrom(source: any = {}) {
	        return new Tag(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.typeCode = source["typeCode"];
	        this.typeName = source["typeName"];
	        this.offset = source["offset"];
	        this.span = this.convertValues(source["span"], Span);
	        this.headerSpan = this.convertValues(source["headerSpan"], Span);
	        this.payloadSpan = this.convertValues(source["payloadSpan"], Span);
	        this.rawTypeFlags = source["rawTypeFlags"];
	        this.filter = source["filter"];
	        this.dataSize = source["dataSize"];
	        this.availableDataSize = source["availableDataSize"];
	        this.dts = source["dts"];
	        this.streamId = source["streamId"];
	        this.payloadComplete = source["payloadComplete"];
	        this.previousTagSize = this.convertValues(source["previousTagSize"], PreviousTagSize);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TagFilter {
	    type: string;
	    dtsMin?: string;
	    dtsMax?: string;
	    structure: string;
	
	    static createFrom(source: any = {}) {
	        return new TagFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.dtsMin = source["dtsMin"];
	        this.dtsMax = source["dtsMax"];
	        this.structure = source["structure"];
	    }
	}
	export class FilteredTagPage {
	    sessionId: string;
	    filter: TagFilter;
	    start: string;
	    next: string;
	    indexedTotal: string;
	    limit: number;
	    scanLimit: number;
	    scanned: number;
	    exhausted: boolean;
	    tags: Tag[];
	
	    static createFrom(source: any = {}) {
	        return new FilteredTagPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.filter = this.convertValues(source["filter"], TagFilter);
	        this.start = source["start"];
	        this.next = source["next"];
	        this.indexedTotal = source["indexedTotal"];
	        this.limit = source["limit"];
	        this.scanLimit = source["scanLimit"];
	        this.scanned = source["scanned"];
	        this.exhausted = source["exhausted"];
	        this.tags = this.convertValues(source["tags"], Tag);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilteredTagPageResponse {
	    apiVersion: number;
	    page?: FilteredTagPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new FilteredTagPageResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.page = this.convertValues(source["page"], FilteredTagPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FilteredTagRequest {
	    sessionId: string;
	    start: string;
	    count: number;
	    filter: TagFilter;
	
	    static createFrom(source: any = {}) {
	        return new FilteredTagRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.start = source["start"];
	        this.count = source["count"];
	        this.filter = this.convertValues(source["filter"], TagFilter);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class HexQuery {
	    sessionId: string;
	    requestId: string;
	
	    static createFrom(source: any = {}) {
	        return new HexQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	    }
	}
	export class HexRequest {
	    sessionId: string;
	    requestId: string;
	    offset: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new HexRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.offset = source["offset"];
	        this.count = source["count"];
	    }
	}
	export class HexWindow {
	    offset: string;
	    sourceSize: string;
	    hex: string;
	    eof: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HexWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.offset = source["offset"];
	        this.sourceSize = source["sourceSize"];
	        this.hex = source["hex"];
	        this.eof = source["eof"];
	    }
	}
	export class HexResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    offset: string;
	    count: number;
	    state: string;
	    window?: HexWindow;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new HexResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.offset = source["offset"];
	        this.count = source["count"];
	        this.state = source["state"];
	        this.window = this.convertValues(source["window"], HexWindow);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class MediaCurveGOP {
	    declaredCount: string;
	    closedCount: string;
	    totalDurationMs: string;
	    minDurationMs?: string;
	    maxDurationMs?: string;
	    averageDurationMs?: string;
	    tagId?: string;
	
	    static createFrom(source: any = {}) {
	        return new MediaCurveGOP(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.declaredCount = source["declaredCount"];
	        this.closedCount = source["closedCount"];
	        this.totalDurationMs = source["totalDurationMs"];
	        this.minDurationMs = source["minDurationMs"];
	        this.maxDurationMs = source["maxDurationMs"];
	        this.averageDurationMs = source["averageDurationMs"];
	        this.tagId = source["tagId"];
	    }
	}
	export class MediaCurveRate {
	    coveredBytes: string;
	    coveredIntervalMs: string;
	    bitsPerSecond?: string;
	    tagId?: string;
	
	    static createFrom(source: any = {}) {
	        return new MediaCurveRate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.coveredBytes = source["coveredBytes"];
	        this.coveredIntervalMs = source["coveredIntervalMs"];
	        this.bitsPerSecond = source["bitsPerSecond"];
	        this.tagId = source["tagId"];
	    }
	}
	export class MediaCurvePoint {
	    index: string;
	    startTag: string;
	    nextTag: string;
	    audio: MediaCurveRate;
	    video: MediaCurveRate;
	    mp3: MediaCurveRate;
	    gop: MediaCurveGOP;
	    unclassifiedTags: string;
	
	    static createFrom(source: any = {}) {
	        return new MediaCurvePoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.startTag = source["startTag"];
	        this.nextTag = source["nextTag"];
	        this.audio = this.convertValues(source["audio"], MediaCurveRate);
	        this.video = this.convertValues(source["video"], MediaCurveRate);
	        this.mp3 = this.convertValues(source["mp3"], MediaCurveRate);
	        this.gop = this.convertValues(source["gop"], MediaCurveGOP);
	        this.unclassifiedTags = source["unclassifiedTags"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MediaCurvePage {
	    bounds: PageBounds;
	    windowTags: number;
	    indexedTags: string;
	    scanComplete: boolean;
	    complete: boolean;
	    points: MediaCurvePoint[];
	
	    static createFrom(source: any = {}) {
	        return new MediaCurvePage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bounds = this.convertValues(source["bounds"], PageBounds);
	        this.windowTags = source["windowTags"];
	        this.indexedTags = source["indexedTags"];
	        this.scanComplete = source["scanComplete"];
	        this.complete = source["complete"];
	        this.points = this.convertValues(source["points"], MediaCurvePoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class MediaCurveRequest {
	    sessionId: string;
	    requestId: string;
	    start: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new MediaCurveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.start = source["start"];
	        this.count = source["count"];
	    }
	}
	export class MediaCurveResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    page?: MediaCurvePage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new MediaCurveResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.page = this.convertValues(source["page"], MediaCurvePage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenRequest {
	    path: string;
	    maxTags: number;
	    maxDiagnostics: number;
	
	    static createFrom(source: any = {}) {
	        return new OpenRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.maxTags = source["maxTags"];
	        this.maxDiagnostics = source["maxDiagnostics"];
	    }
	}
	
	export class PageRequest {
	    sessionId: string;
	    start: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new PageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.start = source["start"];
	        this.count = source["count"];
	    }
	}
	
	export class Progress {
	    sourceSize: string;
	    bytesScanned: string;
	    tagCount: string;
	    diagnosticCount: number;
	
	    static createFrom(source: any = {}) {
	        return new Progress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceSize = source["sourceSize"];
	        this.bytesScanned = source["bytesScanned"];
	        this.tagCount = source["tagCount"];
	        this.diagnosticCount = source["diagnosticCount"];
	    }
	}
	export class ScanOutcome {
	    complete: boolean;
	    hasErrors: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScanOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.complete = source["complete"];
	        this.hasErrors = source["hasErrors"];
	    }
	}
	export class SessionSnapshot {
	    sessionId: string;
	    state: string;
	    terminal: boolean;
	    closed: boolean;
	    progress?: Progress;
	    scan?: ScanOutcome;
	    failure?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new SessionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.state = source["state"];
	        this.terminal = source["terminal"];
	        this.closed = source["closed"];
	        this.progress = this.convertValues(source["progress"], Progress);
	        this.scan = this.convertValues(source["scan"], ScanOutcome);
	        this.failure = this.convertValues(source["failure"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SessionResponse {
	    apiVersion: number;
	    session?: SessionSnapshot;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new SessionResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.session = this.convertValues(source["session"], SessionSnapshot);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SnapshotAVCSearchRequest {
	    requestId: string;
	    start: string;
	    filter: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotAVCSearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.start = source["start"];
	        this.filter = source["filter"];
	    }
	}
	export class SnapshotCodecSearchRequest {
	    requestId: string;
	    cursor: string;
	    kind: string;
	    filter: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotCodecSearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.cursor = source["cursor"];
	        this.kind = source["kind"];
	        this.filter = source["filter"];
	    }
	}
	export class SnapshotMediaCurveRequest {
	    start: string;
	    count: number;
	    metric: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotMediaCurveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.count = source["count"];
	        this.metric = source["metric"];
	    }
	}
	export class SnapshotTrendRequest {
	    start: string;
	    count: number;
	    type: string;
	    metric: string;
	    gapThreshold: string;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotTrendRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.count = source["count"];
	        this.type = source["type"];
	        this.metric = source["metric"];
	        this.gapThreshold = source["gapThreshold"];
	    }
	}
	export class SnapshotSummaryPageRequest {
	    kind: string;
	    start: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotSummaryPageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.start = source["start"];
	        this.count = source["count"];
	    }
	}
	export class SnapshotRequest {
	    schemaVersion?: number;
	    sessionId: string;
	    selectedTagId?: string;
	    detailRequestId?: string;
	    hexRequestId?: string;
	    summaryRequestId?: string;
	    summaryPage?: SnapshotSummaryPageRequest;
	    trend?: SnapshotTrendRequest;
	    mediaCurve?: SnapshotMediaCurveRequest;
	    avcSearch?: SnapshotAVCSearchRequest;
	    codecSearch?: SnapshotCodecSearchRequest;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemaVersion = source["schemaVersion"];
	        this.sessionId = source["sessionId"];
	        this.selectedTagId = source["selectedTagId"];
	        this.detailRequestId = source["detailRequestId"];
	        this.hexRequestId = source["hexRequestId"];
	        this.summaryRequestId = source["summaryRequestId"];
	        this.summaryPage = this.convertValues(source["summaryPage"], SnapshotSummaryPageRequest);
	        this.trend = this.convertValues(source["trend"], SnapshotTrendRequest);
	        this.mediaCurve = this.convertValues(source["mediaCurve"], SnapshotMediaCurveRequest);
	        this.avcSearch = this.convertValues(source["avcSearch"], SnapshotAVCSearchRequest);
	        this.codecSearch = this.convertValues(source["codecSearch"], SnapshotCodecSearchRequest);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	export class SummaryPage {
	    bounds: PageBounds;
	    events: summaryview.Event[];
	
	    static createFrom(source: any = {}) {
	        return new SummaryPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bounds = this.convertValues(source["bounds"], PageBounds);
	        this.events = this.convertValues(source["events"], summaryview.Event);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SummaryPageRequest {
	    sessionId: string;
	    requestId: string;
	    kind: string;
	    start: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new SummaryPageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.kind = source["kind"];
	        this.start = source["start"];
	        this.count = source["count"];
	    }
	}
	export class SummaryPageResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    kind: string;
	    page?: SummaryPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new SummaryPageResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.kind = source["kind"];
	        this.page = this.convertValues(source["page"], SummaryPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SummaryQuery {
	    sessionId: string;
	    requestId: string;
	
	    static createFrom(source: any = {}) {
	        return new SummaryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	    }
	}
	export class SummaryResponse {
	    apiVersion: number;
	    sessionId: string;
	    requestId: string;
	    state: string;
	    processed: string;
	    total: string;
	    overview?: summaryview.Overview;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new SummaryResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.state = source["state"];
	        this.processed = source["processed"];
	        this.total = source["total"];
	        this.overview = this.convertValues(source["overview"], summaryview.Overview);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class TagPage {
	    sessionId: string;
	    bounds: PageBounds;
	    tags: Tag[];
	
	    static createFrom(source: any = {}) {
	        return new TagPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.bounds = this.convertValues(source["bounds"], PageBounds);
	        this.tags = this.convertValues(source["tags"], Tag);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TagPageResponse {
	    apiVersion: number;
	    page?: TagPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new TagPageResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.page = this.convertValues(source["page"], TagPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TagTrendPoint {
	    tagId: string;
	    offset: string;
	    dts: string;
	    typeCode: number;
	    streamId: number;
	    dataSize: number;
	    availableDataSize: number;
	    payloadComplete: boolean;
	    previousTagSizeState: string;
	    filtered: boolean;
	    previousTagId?: string;
	    previousDts?: string;
	    deltaMs?: string;
	    context: string;
	
	    static createFrom(source: any = {}) {
	        return new TagTrendPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tagId = source["tagId"];
	        this.offset = source["offset"];
	        this.dts = source["dts"];
	        this.typeCode = source["typeCode"];
	        this.streamId = source["streamId"];
	        this.dataSize = source["dataSize"];
	        this.availableDataSize = source["availableDataSize"];
	        this.payloadComplete = source["payloadComplete"];
	        this.previousTagSizeState = source["previousTagSizeState"];
	        this.filtered = source["filtered"];
	        this.previousTagId = source["previousTagId"];
	        this.previousDts = source["previousDts"];
	        this.deltaMs = source["deltaMs"];
	        this.context = source["context"];
	    }
	}
	export class TagTrendPage {
	    sessionId: string;
	    bounds: PageBounds;
	    contextStart: string;
	    contextScanned: number;
	    points: TagTrendPoint[];
	
	    static createFrom(source: any = {}) {
	        return new TagTrendPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.bounds = this.convertValues(source["bounds"], PageBounds);
	        this.contextStart = source["contextStart"];
	        this.contextScanned = source["contextScanned"];
	        this.points = this.convertValues(source["points"], TagTrendPoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TagTrendPageResponse {
	    apiVersion: number;
	    page?: TagTrendPage;
	    error?: Fault;
	
	    static createFrom(source: any = {}) {
	        return new TagTrendPageResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.page = this.convertValues(source["page"], TagTrendPage);
	        this.error = this.convertValues(source["error"], Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace detailview {
	
	export class Diagnostic {
	    code: string;
	    severity: string;
	    offset: string;
	    bitOffset: number;
	    message: string;
	    expected?: string;
	    actual?: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.severity = source["severity"];
	        this.offset = source["offset"];
	        this.bitOffset = source["bitOffset"];
	        this.message = source["message"];
	        this.expected = source["expected"];
	        this.actual = source["actual"];
	    }
	}
	export class Span {
	    byteOffset: string;
	    bitOffset: number;
	    bitLength: number;
	
	    static createFrom(source: any = {}) {
	        return new Span(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.byteOffset = source["byteOffset"];
	        this.bitOffset = source["bitOffset"];
	        this.bitLength = source["bitLength"];
	    }
	}
	export class Field {
	    id: string;
	    parentId: string;
	    name: string;
	    type: string;
	    value: string;
	    span?: Span;
	
	    static createFrom(source: any = {}) {
	        return new Field(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parentId = source["parentId"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.value = source["value"];
	        this.span = this.convertValues(source["span"], Span);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Detail {
	    kind: string;
	    complete: boolean;
	    truncated: boolean;
	    fields: Field[];
	    diagnostics: Diagnostic[];
	
	    static createFrom(source: any = {}) {
	        return new Detail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.complete = source["complete"];
	        this.truncated = source["truncated"];
	        this.fields = this.convertValues(source["fields"], Field);
	        this.diagnostics = this.convertValues(source["diagnostics"], Diagnostic);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	

}

export namespace main {
	
	export class PickResponse {
	    apiVersion: number;
	    requestId: string;
	    pending: boolean;
	    canceled: boolean;
	    fileName: string;
	    session?: desktop.SessionSnapshot;
	    error?: desktop.Fault;
	
	    static createFrom(source: any = {}) {
	        return new PickResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.requestId = source["requestId"];
	        this.pending = source["pending"];
	        this.canceled = source["canceled"];
	        this.fileName = source["fileName"];
	        this.session = this.convertValues(source["session"], desktop.SessionSnapshot);
	        this.error = this.convertValues(source["error"], desktop.Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveRequest {
	    requestId: string;
	    snapshot: desktop.SnapshotRequest;
	
	    static createFrom(source: any = {}) {
	        return new SaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.snapshot = this.convertValues(source["snapshot"], desktop.SnapshotRequest);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SaveResponse {
	    apiVersion: number;
	    requestId: string;
	    pending: boolean;
	    canceled: boolean;
	    fileName: string;
	    bytesWritten: string;
	    error?: desktop.Fault;
	
	    static createFrom(source: any = {}) {
	        return new SaveResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.apiVersion = source["apiVersion"];
	        this.requestId = source["requestId"];
	        this.pending = source["pending"];
	        this.canceled = source["canceled"];
	        this.fileName = source["fileName"];
	        this.bytesWritten = source["bytesWritten"];
	        this.error = this.convertValues(source["error"], desktop.Fault);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace summaryview {
	
	export class Metric {
	    section: string;
	    name: string;
	    value?: string;
	    unit: string;
	
	    static createFrom(source: any = {}) {
	        return new Metric(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.section = source["section"];
	        this.name = source["name"];
	        this.value = source["value"];
	        this.unit = source["unit"];
	    }
	}
	export class Event {
	    id: string;
	    tagId?: string;
	    offset: string;
	    bitOffset: number;
	    dts?: string;
	    title: string;
	    severity: string;
	    message: string;
	    values: Metric[];
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tagId = source["tagId"];
	        this.offset = source["offset"];
	        this.bitOffset = source["bitOffset"];
	        this.dts = source["dts"];
	        this.title = source["title"];
	        this.severity = source["severity"];
	        this.message = source["message"];
	        this.values = this.convertValues(source["values"], Metric);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ListInfo {
	    kind: string;
	    observed?: string;
	    retained: string;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ListInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.observed = source["observed"];
	        this.retained = source["retained"];
	        this.truncated = source["truncated"];
	    }
	}
	
	export class Overview {
	    sourceSize: string;
	    indexedTags: string;
	    scanComplete: boolean;
	    scanHasErrors: boolean;
	    complete: boolean;
	    hasErrors: boolean;
	    metrics: Metric[];
	    lists: ListInfo[];
	
	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceSize = source["sourceSize"];
	        this.indexedTags = source["indexedTags"];
	        this.scanComplete = source["scanComplete"];
	        this.scanHasErrors = source["scanHasErrors"];
	        this.complete = source["complete"];
	        this.hasErrors = source["hasErrors"];
	        this.metrics = this.convertValues(source["metrics"], Metric);
	        this.lists = this.convertValues(source["lists"], ListInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

