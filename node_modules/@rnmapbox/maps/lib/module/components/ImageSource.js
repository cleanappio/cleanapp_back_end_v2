"use strict";

import React from 'react';
import { cloneReactChildrenWithProps, isNumber, resolveImagePath } from '../utils';
import RNMBXImageSourceNativeComponent from '../specs/RNMBXImageSourceNativeComponent';
import AbstractSource from './AbstractSource';
import { jsx as _jsx } from "react/jsx-runtime";
export const NATIVE_MODULE_NAME = 'RNMBXImageSource';
/**
 * ImageSource is a content source that is used for a georeferenced raster image to be shown on the map.
 * The georeferenced image scales and rotates as the user zooms and rotates the map
 */
class ImageSource extends AbstractSource {
  _getURL() {
    const {
      url
    } = this.props;
    if (isNumber(url)) {
      return resolveImagePath(url);
    } else {
      return url;
    }
  }
  render() {
    if (!this.props.url || !this.props.coordinates || !this.props.coordinates.length) {
      return null;
    }
    const props = {
      ...this.props,
      url: this._getURL()
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXImageSourceNativeComponent, {
        ref: this.setNativeRef,
        ...props,
        children: cloneReactChildrenWithProps(this.props.children, {
          sourceID: this.props.id
        })
      })
    );
  }
}
export default ImageSource;
//# sourceMappingURL=ImageSource.js.map