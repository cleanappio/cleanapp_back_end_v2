"use strict";

import React from 'react';
import { NativeModules, Platform } from 'react-native';
import RNMBXMakerViewContentCoponent from '../specs/RNMBXMarkerViewContentNativeComponent';
import NativeMarkerViewComponent from '../specs/RNMBXMarkerViewNativeComponent';
import { toJSONString } from '../utils';
import { makePoint } from '../utils/geoUtils';
import PointAnnotation from './PointAnnotation';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;
/**
 * MarkerView represents an interactive React Native marker on the map.
 *
 * If you have static views, consider using PointAnnotation or SymbolLayer to display
 * an image, as they'll offer much better performance. Mapbox suggests using this
 * component for a maximum of around 100 views displayed at one time.
 *
 * This is implemented with view annotations on [Android](https://docs.mapbox.com/android/maps/guides/annotations/view-annotations/)
 * and [iOS](https://docs.mapbox.com/ios/maps/guides/annotations/view-annotations).
 *
 * This component has no dedicated `onPress` method. Instead, you should handle gestures
 * with the React views passed in as `children`.
 */
class MarkerView extends React.PureComponent {
  static defaultProps = {
    anchor: {
      x: 0.5,
      y: 0.5
    },
    allowOverlap: false,
    allowOverlapWithPuck: false,
    isSelected: false
  };
  static lastId = 0;
  _idForPointAnnotation() {
    if (this.__idForPointAnnotation === undefined) {
      MarkerView.lastId = MarkerView.lastId + 1;
      this.__idForPointAnnotation = `MV-${MarkerView.lastId}`;
    }
    return this.__idForPointAnnotation;
  }
  _getCoordinate(coordinate) {
    if (!coordinate) {
      return undefined;
    }
    return toJSONString(makePoint(coordinate));
  }
  render() {
    if (this.props.anchor.x < 0 || this.props.anchor.y < 0 || this.props.anchor.x > 1 || this.props.anchor.y > 1) {
      console.warn(`[MarkerView] Anchor with value (${this.props.anchor.x}, ${this.props.anchor.y}) should not be outside the range [(0, 0), (1, 1)]`);
    }
    if (Platform.OS === 'ios' && !Mapbox.MapboxV10) {
      return /*#__PURE__*/_jsx(PointAnnotation, {
        id: this._idForPointAnnotation(),
        ...this.props
      });
    }
    const {
      anchor = {
        x: 0.5,
        y: 0.5
      }
    } = this.props;
    return /*#__PURE__*/_jsx(RNMBXMarkerView, {
      style: [{
        flex: 0,
        alignSelf: 'flex-start'
      }, this.props.style],
      coordinate: [Number(this.props.coordinate[0]), Number(this.props.coordinate[1])],
      anchor: anchor,
      allowOverlap: this.props.allowOverlap,
      allowOverlapWithPuck: this.props.allowOverlapWithPuck,
      isSelected: this.props.isSelected,
      onTouchEnd: e => {
        e.stopPropagation();
      },
      children: /*#__PURE__*/_jsx(RNMBXMakerViewContentCoponent, {
        style: {
          flex: 0,
          alignSelf: 'flex-start'
        },
        onStartShouldSetResponder: _event => {
          return true;
        },
        onTouchEnd: e => {
          e.stopPropagation();
        },
        children: this.props.children
      })
    });
  }
}
const RNMBXMarkerView = NativeMarkerViewComponent;
export default MarkerView;
//# sourceMappingURL=MarkerView.js.map