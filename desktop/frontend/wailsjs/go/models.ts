export namespace desktop {
	
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
	
	    static createFrom(source: any = {}) {
	        return new DetailRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.requestId = source["requestId"];
	        this.tagId = source["tagId"];
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

