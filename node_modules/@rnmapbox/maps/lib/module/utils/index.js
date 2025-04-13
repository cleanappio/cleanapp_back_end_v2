"use strict";

/* eslint-disable @typescript-eslint/no-explicit-any */
import React from 'react';
import { findNodeHandle, Platform, Image } from 'react-native';
export function isAndroid() {
  return Platform.OS === 'android';
}
export function existenceChange(cur, next) {
  if (!cur && !next) {
    return false;
  }
  return !cur && next || cur && !next;
}
export function isFunction(fn) {
  return typeof fn === 'function';
}
export function isNumber(num) {
  return typeof num === 'number' && !Number.isNaN(num);
}
export function isUndefined(obj) {
  return typeof obj === 'undefined';
}
export function isString(str) {
  return typeof str === 'string';
}
export function isBoolean(bool) {
  return typeof bool === 'boolean';
}
export function isPrimitive(value) {
  return isString(value) || isNumber(value) || isBoolean(value);
}
export function runNativeMethod(turboModule, name, nativeRef, args) {
  const handle = findNodeHandle(nativeRef);
  if (!handle) {
    throw new Error(`Could not find handle for native ref ${module}.${name}`);
  }

  // @ts-expect-error TS says that string cannot be used to index Turbomodules.
  // It can, it's just not pretty.
  return turboModule[name](handle, ...args);
}
export function cloneReactChildrenWithProps(children, propsToAdd = {}) {
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
  return React.Children.map(filteredChildren, child => /*#__PURE__*/React.cloneElement(child, propsToAdd));
}
export function resolveImagePath(imageRef) {
  const res = Image.resolveAssetSource(imageRef);
  return res.uri;
}
export function toJSONString(json = '') {
  return JSON.stringify(json);
}
//# sourceMappingURL=index.js.map