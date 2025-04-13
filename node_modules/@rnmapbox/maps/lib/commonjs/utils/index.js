"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.cloneReactChildrenWithProps = cloneReactChildrenWithProps;
exports.existenceChange = existenceChange;
exports.isAndroid = isAndroid;
exports.isBoolean = isBoolean;
exports.isFunction = isFunction;
exports.isNumber = isNumber;
exports.isPrimitive = isPrimitive;
exports.isString = isString;
exports.isUndefined = isUndefined;
exports.resolveImagePath = resolveImagePath;
exports.runNativeMethod = runNativeMethod;
exports.toJSONString = toJSONString;
var _react = _interopRequireDefault(require("react"));
var _reactNative = require("react-native");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
/* eslint-disable @typescript-eslint/no-explicit-any */

function isAndroid() {
  return _reactNative.Platform.OS === 'android';
}
function existenceChange(cur, next) {
  if (!cur && !next) {
    return false;
  }
  return !cur && next || cur && !next;
}
function isFunction(fn) {
  return typeof fn === 'function';
}
function isNumber(num) {
  return typeof num === 'number' && !Number.isNaN(num);
}
function isUndefined(obj) {
  return typeof obj === 'undefined';
}
function isString(str) {
  return typeof str === 'string';
}
function isBoolean(bool) {
  return typeof bool === 'boolean';
}
function isPrimitive(value) {
  return isString(value) || isNumber(value) || isBoolean(value);
}
function runNativeMethod(turboModule, name, nativeRef, args) {
  const handle = (0, _reactNative.findNodeHandle)(nativeRef);
  if (!handle) {
    throw new Error(`Could not find handle for native ref ${module}.${name}`);
  }

  // @ts-expect-error TS says that string cannot be used to index Turbomodules.
  // It can, it's just not pretty.
  return turboModule[name](handle, ...args);
}
function cloneReactChildrenWithProps(children, propsToAdd = {}) {
  if (!children) {
    return null;
  }
  let foundChildren = null;
  if (!Array.isArray(children)) {
    foundChildren = [children];
  } else {
    foundChildren = children;
  }
  const filteredChildren = foundChildren.filter(child => !!child); // filter out falsy children, since some can be null
  return _react.default.Children.map(filteredChildren, child => /*#__PURE__*/_react.default.cloneElement(child, propsToAdd));
}
function resolveImagePath(imageRef) {
  const res = _reactNative.Image.resolveAssetSource(imageRef);
  return res.uri;
}
function toJSONString(json = '') {
  return JSON.stringify(json);
}
//# sourceMappingURL=index.js.map