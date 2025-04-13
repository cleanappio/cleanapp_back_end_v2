"use strict";

import React from 'react';
import { StyleSheet } from 'react-native';
import { toJSONString, isFunction } from '../utils';
import checkRequiredProps from '../utils/checkRequiredProps';
import { makePoint } from '../utils/geoUtils';
import RNMBXPointAnnotationNativeComponent from '../specs/RNMBXPointAnnotationNativeComponent';
import NativeRNMBXPointAnnotationModule from '../specs/NativeRNMBXPointAnnotationModule';
import NativeBridgeComponent from './NativeBridgeComponent';
import { jsx as _jsx } from "react/jsx-runtime";
export const NATIVE_MODULE_NAME = 'RNMBXPointAnnotation';
const styles = StyleSheet.create({
  container: {
    alignItems: 'center',
    justifyContent: 'center',
    position: 'absolute'
  }
});
/**
 * PointAnnotation represents a one-dimensional shape located at a single geographical coordinate.
 *
 * Consider using ShapeSource and SymbolLayer instead, if you have many points and static images,
 * they'll offer much better performance.
 *
 * If you need interactive views please use MarkerView because PointAnnotation will render children onto a bitmap.
 * Also disable any kind of animations like `fadeDuration` of `Image`.
 * Otherwise, the bitmap might be rendered at an unknown state of the animation.
 */
class PointAnnotation extends NativeBridgeComponent(React.PureComponent, NativeRNMBXPointAnnotationModule) {
  static defaultProps = {
    anchor: {
      x: 0.5,
      y: 0.5
    },
    draggable: false
  };
  _nativeRef = null;
  constructor(props) {
    super(props);
    checkRequiredProps('PointAnnotation', props, ['id', 'coordinate']);
    this._onSelected = this._onSelected.bind(this);
    this._onDeselected = this._onDeselected.bind(this);
    this._onDragStart = this._onDragStart.bind(this);
    this._onDrag = this._onDrag.bind(this);
    this._onDragEnd = this._onDragEnd.bind(this);
  }
  _decodePayload(payload) {
    // we check whether the payload is a string, since the strict type safety is enforced only on iOS on the new arch
    // on Android, on both archs, the payload is an object
    if (typeof payload === 'string') {
      return JSON.parse(payload);
    } else {
      return payload;
    }
  }
  _onSelected(e) {
    if (isFunction(this.props.onSelected)) {
      const payload = this._decodePayload(e.nativeEvent.payload);
      this.props.onSelected(payload);
    }
  }
  _onDeselected(e) {
    if (isFunction(this.props.onDeselected)) {
      const payload = this._decodePayload(e.nativeEvent.payload);
      this.props.onDeselected(payload);
    }
  }
  _onDragStart(e) {
    if (isFunction(this.props.onDragStart)) {
      const payload = this._decodePayload(e.nativeEvent.payload);
      this.props.onDragStart(payload);
    }
  }
  _onDrag(e) {
    if (isFunction(this.props.onDrag)) {
      const payload = this._decodePayload(e.nativeEvent.payload);
      this.props.onDrag(payload);
    }
  }
  _onDragEnd(e) {
    if (isFunction(this.props.onDragEnd)) {
      const payload = this._decodePayload(e.nativeEvent.payload);
      this.props.onDragEnd(payload);
    }
  }
  _getCoordinate() {
    if (!this.props.coordinate) {
      return undefined;
    }
    return toJSONString(makePoint(this.props.coordinate));
  }

  /**
   * On v10 and pre v10 android point annotation is rendered offscreen with a canvas into an image.
   * To rerender the image from the current state of the view call refresh.
   * Call this for example from Image#onLoad.
   */
  refresh() {
    this._runNativeMethod('refresh', this._nativeRef, []);
  }
  _setNativeRef(nativeRef) {
    this._nativeRef = nativeRef;
    super._runPendingNativeMethods(nativeRef);
  }
  render() {
    const props = {
      ...this.props,
      ref: nativeRef => this._setNativeRef(nativeRef),
      id: this.props.id,
      title: this.props.title,
      snippet: this.props.snippet,
      anchor: this.props.anchor,
      selected: this.props.selected,
      draggable: this.props.draggable,
      style: [this.props.style, styles.container],
      onMapboxPointAnnotationSelected: this._onSelected,
      onMapboxPointAnnotationDeselected: this._onDeselected,
      onMapboxPointAnnotationDragStart: this._onDragStart,
      onMapboxPointAnnotationDrag: this._onDrag,
      onMapboxPointAnnotationDragEnd: this._onDragEnd,
      coordinate: this._getCoordinate()
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXPointAnnotationNativeComponent, {
        ...props,
        children: this.props.children
      })
    );
  }
}
export default PointAnnotation;
//# sourceMappingURL=PointAnnotation.js.map