export namespace main {
	
	export class Asset {
	    id: string;
	    name: string;
	    kind: string;
	    url: string;
	    size: number;
	    created: string;
	
	    static createFrom(source: any = {}) {
	        return new Asset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.url = source["url"];
	        this.size = source["size"];
	        this.created = source["created"];
	    }
	}
	export class BridgeStatus {
	    connected: boolean;
	    serial: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new BridgeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.serial = source["serial"];
	        this.detail = source["detail"];
	    }
	}
	export class ExportLimits {
	    maxSeconds: number;
	    maxBytes: number;
	    motionKbps: number;
	    stillKbps: number;
	    headroom: number;
	    renderFps: number;
	
	    static createFrom(source: any = {}) {
	        return new ExportLimits(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxSeconds = source["maxSeconds"];
	        this.maxBytes = source["maxBytes"];
	        this.motionKbps = source["motionKbps"];
	        this.stillKbps = source["stillKbps"];
	        this.headroom = source["headroom"];
	        this.renderFps = source["renderFps"];
	    }
	}
	export class ProjectSave {
	    id: string;
	    name: string;
	    kind: string;
	    duration: number;
	    data: string;
	    thumb: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectSave(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.duration = source["duration"];
	        this.data = source["data"];
	        this.thumb = source["thumb"];
	    }
	}
	export class ExportRequest {
	    exportId: string;
	    motion: boolean;
	    show: boolean;
	    save: ProjectSave;
	
	    static createFrom(source: any = {}) {
	        return new ExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exportId = source["exportId"];
	        this.motion = source["motion"];
	        this.show = source["show"];
	        this.save = this.convertValues(source["save"], ProjectSave);
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
	export class MediaItem {
	    id: string;
	    name: string;
	    remoteName: string;
	    created: string;
	    thumb: string;
	    kind: string;
	    draft: boolean;
	    editable: boolean;
	    duration: number;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new MediaItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.remoteName = source["remoteName"];
	        this.created = source["created"];
	        this.thumb = source["thumb"];
	        this.kind = source["kind"];
	        this.draft = source["draft"];
	        this.editable = source["editable"];
	        this.duration = source["duration"];
	        this.size = source["size"];
	    }
	}
	export class ExportResult {
	    item: MediaItem;
	    shown: boolean;
	    showError: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.item = this.convertValues(source["item"], MediaItem);
	        this.shown = source["shown"];
	        this.showError = source["showError"];
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
	export class ImportResult {
	    assets: Asset[];
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assets = this.convertValues(source["assets"], Asset);
	        this.errors = source["errors"];
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
	export class JobProgress {
	    active: boolean;
	    label: string;
	    percent: number;
	
	    static createFrom(source: any = {}) {
	        return new JobProgress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.label = source["label"];
	        this.percent = source["percent"];
	    }
	}
	
	
	export class YouTubeRequest {
	    url: string;
	    start: number;
	    end: number;
	
	    static createFrom(source: any = {}) {
	        return new YouTubeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.start = source["start"];
	        this.end = source["end"];
	    }
	}
	export class YouTubeTool {
	    installed: boolean;
	    managed: boolean;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new YouTubeTool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.managed = source["managed"];
	        this.path = source["path"];
	    }
	}

}

