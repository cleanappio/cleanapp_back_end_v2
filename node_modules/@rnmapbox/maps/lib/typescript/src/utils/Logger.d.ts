import { EmitterSubscription, NativeEventEmitter } from 'react-native';
export type LogLevel = 'error' | 'warning' | 'info' | 'debug' | 'verbose';
export interface LogObject {
    level: LogLevel;
    tag: string;
    message: string;
}
type LogCallback = (object: LogObject) => boolean;
declare class Logger {
    static instance: Logger | null;
    loggerEmitter: NativeEventEmitter;
    startedCount: number;
    logCallback?: LogCallback;
    subscription?: EmitterSubscription;
    static sharedInstance(): Logger;
    constructor();
    /**
     * Set custom logger function.
     * @param {Logger~logCallback} logCallback - callback taking a log object as param. If callback return falsy value then
     * default logging will take place.
     */
    static setLogCallback(logCallback: LogCallback): void;
    /**
     * Set custom logger function.
     * @param {Logger~logCallback} logCallback - callback taking a log object as param. If callback return falsy value then
     * default logging will take place.
     */
    setLogCallback(logCallback: LogCallback): void;
    /**
     * This callback is displayed as part of the Requester class.
     * @callback Logger~logCallback
     * @param {object} log
     * @param {string} log.level - log level
     * @param {string} log.tag - optional tag used on android
     * @param {string} log.message - the message of the log
     */
    /**
     * setLogLevel
     * @param {LogLevel} level
     */
    static setLogLevel(level: LogLevel): void;
    /**
     * @type {('error'|'warning'|'info'|'debug'|'verbose')} LogLevel - Supported log levels
     */
    start(): void;
    stop(): void;
    subscribe(): void;
    unsubscribe(): void;
    effectiveLevel(log: LogObject): LogLevel;
    onLog(log: LogObject): void;
}
export default Logger;
//# sourceMappingURL=Logger.d.ts.map